package gedis

import (
	"testing"
)

func TestTSRevRangeWithAgg(t *testing.T) {
	db := New()

	db.TSAddWithLabels("ts_rev_agg", 1000, 1.0, map[string]string{"sensor": "temp"})
	db.TSAddWithLabels("ts_rev_agg", 2000, 2.0, map[string]string{"sensor": "temp"})
	db.TSAddWithLabels("ts_rev_agg", 3000, 3.0, map[string]string{"sensor": "temp"})

	result := db.TSRevRangeWithAgg("ts_rev_agg", 1000, 3000, "avg", 1000)
	if len(result) == 0 {
		t.Error("expected non-empty result")
	}
}

func TestTSDeleteRule(t *testing.T) {
	db := New()

	db.TSAddWithLabels("ts_source", 1000, 1.0, map[string]string{"type": "raw"})
	db.TSAddWithLabels("ts_dest", 1000, 1.0, map[string]string{"type": "agg"})

	result := db.TSCreateRule("ts_source", "ts_dest", 1000, "avg")
	if !result {
		t.Fatal("TSCreateRule failed")
	}

	result = db.TSDeleteRule("ts_source", "ts_dest")
	if !result {
		t.Error("expected TSDeleteRule to return true")
	}
}

func TestTSAggregateFunc(t *testing.T) {
	db := New()

	points := []TSPoint{
		{Timestamp: 1000, Value: 1.0},
		{Timestamp: 2000, Value: 2.0},
		{Timestamp: 3000, Value: 3.0},
	}

	sum := db.TSAggregate(points, "sum")
	if sum != 6.0 {
		t.Errorf("expected sum 6.0, got %f", sum)
	}

	avg := db.TSAggregate(points, "avg")
	if avg != 2.0 {
		t.Errorf("expected avg 2.0, got %f", avg)
	}

	min := db.TSAggregate(points, "min")
	if min != 1.0 {
		t.Errorf("expected min 1.0, got %f", min)
	}

	max := db.TSAggregate(points, "max")
	if max != 3.0 {
		t.Errorf("expected max 3.0, got %f", max)
	}
}

func TestTSAggregateFirstLastLowCov(t *testing.T) {
	db := New()

	points := []TSPoint{
		{Timestamp: 1000, Value: 1.0},
		{Timestamp: 2000, Value: 2.0},
		{Timestamp: 3000, Value: 3.0},
	}

	first := db.TSAggregate(points, "first")
	if first != 1.0 {
		t.Errorf("expected first 1.0, got %f", first)
	}

	last := db.TSAggregate(points, "last")
	if last != 3.0 {
		t.Errorf("expected last 3.0, got %f", last)
	}

	count := db.TSAggregate(points, "count")
	if count != 3.0 {
		t.Errorf("expected count 3.0, got %f", count)
	}

	rangeVal := db.TSAggregate(points, "range")
	if rangeVal != 2.0 {
		t.Errorf("expected range 2.0, got %f", rangeVal)
	}
}

func TestTSAggregateEmpty(t *testing.T) {
	db := New()

	points := []TSPoint{}

	result := db.TSAggregate(points, "sum")
	if result != 0 {
		t.Errorf("expected 0 for empty points, got %f", result)
	}
}

func TestTimeSeriesBasic(t *testing.T) {
	db := New()

	for i := 0; i < 10; i++ {
		db.TSAdd("ts_basic", int64(i*1000), float64(i))
	}

	result := db.TSRange("ts_basic", 0, 10000)
	if len(result) != 10 {
		t.Errorf("expected 10 points, got %d", len(result))
	}

	result = db.TSRevRange("ts_basic", 0, 10000)
	if len(result) != 10 {
		t.Errorf("expected 10 points in reverse, got %d", len(result))
	}
}

func TestTSAggregationAll(t *testing.T) {
	db := New()

	points := []TSPoint{
		{Timestamp: 1000, Value: 1.0},
		{Timestamp: 2000, Value: 2.0},
		{Timestamp: 3000, Value: 3.0},
		{Timestamp: 4000, Value: 4.0},
		{Timestamp: 5000, Value: 5.0},
	}

	tests := []struct {
		name     string
		agg      TSAggregation
		expected float64
	}{
		{"sum", "sum", 15.0},
		{"avg", "avg", 3.0},
		{"min", "min", 1.0},
		{"max", "max", 5.0},
		{"count", "count", 5.0},
		{"first", "first", 1.0},
		{"last", "last", 5.0},
		{"range", "range", 4.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := db.TSAggregate(points, tt.agg)
			if result != tt.expected {
				t.Errorf("TSAggregate(%s) = %v, want %v", tt.agg, result, tt.expected)
			}
		})
	}
}

func TestTSDownsample(t *testing.T) {
	db := New()

	points := []TSPoint{
		{Timestamp: 1000, Value: 1.0},
		{Timestamp: 1500, Value: 2.0},
		{Timestamp: 2000, Value: 3.0},
		{Timestamp: 2500, Value: 4.0},
		{Timestamp: 3000, Value: 5.0},
	}

	result := db.tsDownsample(points, 2000, "avg")
	if len(result) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(result))
	}

	if result[0].Value != 1.5 {
		t.Errorf("expected first bucket avg 1.5, got %f", result[0].Value)
	}
	if result[1].Value != 4.0 {
		t.Errorf("expected second bucket avg 4.0, got %f", result[1].Value)
	}
}

func TestTSDownsampleSum(t *testing.T) {
	db := New()

	points := []TSPoint{
		{Timestamp: 1000, Value: 1.0},
		{Timestamp: 1500, Value: 2.0},
		{Timestamp: 2000, Value: 3.0},
		{Timestamp: 2500, Value: 4.0},
	}

	result := db.tsDownsample(points, 2000, "sum")
	if len(result) != 2 {
		t.Errorf("expected 2 buckets, got %d", len(result))
	}

	if result[0].Value != 3.0 {
		t.Errorf("expected first bucket sum 3.0, got %f", result[0].Value)
	}
	if result[1].Value != 7.0 {
		t.Errorf("expected second bucket sum 7.0, got %f", result[1].Value)
	}
}

func TestTSDownsampleEmpty(t *testing.T) {
	db := New()

	points := []TSPoint{}
	result := db.tsDownsample(points, 1000, "avg")
	if len(result) != 0 {
		t.Errorf("expected 0 for empty points, got %d", len(result))
	}

	points2 := []TSPoint{{Timestamp: 1000, Value: 1.0}}
	result2 := db.tsDownsample(points2, 0, "avg")
	if len(result2) != 1 {
		t.Errorf("expected 1 for zero bucket size, got %d", len(result2))
	}
}
