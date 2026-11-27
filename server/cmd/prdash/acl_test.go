package main

import (
	"slices"
	"strings"
	"testing"
)

const (
	sidUser   = "S-1-5-21-1-2-3-1001"
	sidSystem = "S-1-5-18"
	sidAdmins = "S-1-5-32-544"
	sidUsers  = "S-1-5-32-545"
	sidEvery  = "S-1-1-0"
)

func TestStrangers(t *testing.T) {
	allowed := []string{sidUser, sidSystem, sidAdmins}
	cases := []struct {
		name   string
		grants []grant
		want   []string
	}{
		{"none", nil, nil},
		{"owner only", []grant{{sidUser, 0x1f01ff}}, nil},
		{"the three principals", []grant{{sidUser, 1}, {sidSystem, 1}, {sidAdmins, 1}, {sidUser, 2}}, nil},
		{"users group", []grant{{sidUser, 1}, {sidUsers, 0x1200a9}}, []string{sidUsers}},
		{"everyone, once", []grant{{sidEvery, 1}, {sidAdmins, 1}, {sidEvery, 2}}, []string{sidEvery}},
		{"empty mask grants nothing", []grant{{sidEvery, 0}}, nil},
		{"order kept", []grant{{sidEvery, 1}, {sidUsers, 1}}, []string{sidEvery, sidUsers}},
	}
	for _, tc := range cases {
		if got := strangers(tc.grants, allowed...); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestACLErrorNamesThePathAndTheFix(t *testing.T) {
	err := aclError(`C:\Users\me\.prdash`, []string{sidUsers})
	for _, want := range []string{`C:\Users\me\.prdash`, sidUsers, "icacls", "/inheritance:r"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing from %v", want, err)
		}
	}
}
