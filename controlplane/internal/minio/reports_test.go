package minio

import "testing"

func TestHasReportSuffix(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"mysql-pod-delete-20260521-001523/mysql-pod-delete-20260521-001523-main-123/verdict/report.json", true},
		{"mysql-pod-delete-20260521-001523/something-else.txt", false},
		{"", false},
		{"verdict/report.json", true},
	}
	for _, c := range cases {
		if got := hasReportSuffix(c.key); got != c.want {
			t.Errorf("hasReportSuffix(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
