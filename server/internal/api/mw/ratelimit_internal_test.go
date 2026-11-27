package mw

import (
	"testing"
	"time"
)

func TestBucketsDropIdleEntriesAndKeepActiveOnes(t *testing.T) {
	b := newBuckets(30)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, k := range []string{"a", "b", "c"} {
		b.take(k, now)
	}
	if b.size() != 3 {
		t.Fatalf("size %d", b.size())
	}
	// Before a refill period has passed nothing is swept.
	b.take("a", now.Add(30*time.Second))
	if b.size() != 3 {
		t.Fatalf("size %d after 30s", b.size())
	}
	// "a" was used 31s ago, "b" and "c" a minute ago: only the idle ones go.
	b.take("d", now.Add(61*time.Second))
	if _, ok := b.m["a"]; !ok || b.size() != 2 {
		t.Fatalf("size %d, a kept %v", b.size(), ok)
	}
	// An evicted key starts with a full bucket, as it would have had after refilling.
	for range 30 {
		if d := b.take("b", now.Add(62*time.Second)); d != 0 {
			t.Fatalf("delay %v", d)
		}
	}
	if d := b.take("b", now.Add(62*time.Second)); d == 0 {
		t.Fatal("31st take must wait")
	}
}

func TestBucketsSweepRunsOncePerRefillPeriod(t *testing.T) {
	b := newBuckets(30)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.take("a", now)
	b.take("x", now.Add(time.Minute))
	swept := b.lastSweep
	b.take("y", now.Add(time.Minute+time.Second))
	if !b.lastSweep.Equal(swept) {
		t.Fatal("swept again within the period")
	}
}
