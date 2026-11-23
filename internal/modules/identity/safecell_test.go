package identity

import "testing"

func TestSafeCell(t *testing.T) {
	cases := map[string]string{
		"Jane":                    "Jane",
		"=HYPERLINK(\"x\",\"y\")": "'=HYPERLINK(\"x\",\"y\")",
		"+8801700000000":          "'+8801700000000",
		"@SUM(A1)":                "'@SUM(A1)",
		"":                        "",
	}
	for in, want := range cases {
		if got := safeCell(in); got != want {
			t.Errorf("safeCell(%q) = %q, want %q", in, got, want)
		}
	}
}
