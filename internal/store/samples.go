package store

import (
	"fmt"
	"strings"
	"time"
)

// 指标时序采样：每分钟把全部规则的当前值存一个点，给趋势图用。
// 规模账：~320 条规则 × 1440 点/天 × 3 天 ≈ 140 万行，SQLite 单表
// append + (check_id, ts) 索引没有压力；查询时按 5 分钟分桶降采样。

type MetricPoint struct {
	Ts    int64   `json:"t"` // unix 秒（分桶后为桶起点）
	Value float64 `json:"v"`
}

// InsertMetricSamples 批量写入一轮采样（单事务）。
func (s *Store) InsertMetricSamples(ts int64, samples map[int64]float64) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO metric_samples (check_id, ts, value) VALUES (?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for id, v := range samples {
		if _, err := stmt.Exec(id, ts, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ListMetricSamples 返回单条规则的时序，按 bucketSec 分桶取均值。
func (s *Store) ListMetricSamples(checkID int64, since time.Time, bucketSec int) ([]MetricPoint, error) {
	if bucketSec <= 0 {
		bucketSec = 300
	}
	rows, err := s.db.Query(fmt.Sprintf(`SELECT (ts/%d)*%d AS bucket, AVG(value)
		FROM metric_samples WHERE check_id = ? AND ts >= ?
		GROUP BY bucket ORDER BY bucket`, bucketSec, bucketSec),
		checkID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if rows.Scan(&p.Ts, &p.Value) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// ListMetricSamplesByChecks 批量取多条规则的时序（对象详情页一次拉全）。
func (s *Store) ListMetricSamplesByChecks(checkIDs []int64, since time.Time, bucketSec int) (map[int64][]MetricPoint, error) {
	out := map[int64][]MetricPoint{}
	if len(checkIDs) == 0 {
		return out, nil
	}
	if bucketSec <= 0 {
		bucketSec = 300
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(checkIDs)), ",")
	args := make([]any, 0, len(checkIDs)+1)
	for _, id := range checkIDs {
		args = append(args, id)
	}
	args = append(args, since.Unix())
	rows, err := s.db.Query(fmt.Sprintf(`SELECT check_id, (ts/%d)*%d AS bucket, AVG(value)
		FROM metric_samples WHERE check_id IN (%s) AND ts >= ?
		GROUP BY check_id, bucket ORDER BY check_id, bucket`, bucketSec, bucketSec, ph), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var p MetricPoint
		if rows.Scan(&id, &p.Ts, &p.Value) == nil {
			out[id] = append(out[id], p)
		}
	}
	return out, rows.Err()
}

// PurgeOldMetricSamples 只保留最近 72 小时。
func (s *Store) PurgeOldMetricSamples() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM metric_samples WHERE ts < ?`,
		time.Now().Add(-72*time.Hour).Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
