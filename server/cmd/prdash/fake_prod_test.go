//go:build !e2e

package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestFakeFlagDoesNotExistInTheReleaseBuild(t *testing.T) {
	if _, err := parseFlags([]string{"--fake"}, io.Discard); err == nil {
		t.Fatal("--fake accepted by the release build")
	}
	if startFakes != nil || extraFlags != nil {
		t.Fatal("fake hooks are compiled into the release build")
	}
}

func TestRunRefusesFakeWithoutTheE2EBuild(t *testing.T) {
	dir := newDataDir(t)
	err := run(context.Background(), options{port: freePort(t), dataDir: dir, fake: true}, testEnv(&syncBuffer{}, &syncBuffer{}, nil))
	if err == nil || !strings.Contains(err.Error(), "--fake") {
		t.Fatalf("err %v", err)
	}
}
