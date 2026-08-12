package main

import (
	"context"
	"testing"
)

func TestDigestMatchesOpenSSLFormat(t *testing.T) {
	if got := digest(context.Background(), "alice"); got != "408b27d3097eea5a46bf2ab6433a7234a33d5e49957b13ec7acc2ca08e1a13c75272c90c8d3385d47ede5420a7a9623aad817d9f8a70bd100a0acea7400daa59" {
		t.Fatalf("digest(alice) = %q", got)
	}
}

func TestPasshashFormatMatchesOpenSSL(t *testing.T) {
	salt := digest(context.Background(), "alice")
	got := digest(context.Background(), "password:"+salt)
	want := "02f563e501ffd19fe110ac520c437972a19c66fbea636d49a9758c2b0bebc795a726303cdcf915db0d1e0ee3e1f922c0dd1ab53f2701cd2aa3c3258d1ea2818c"
	if got != want {
		t.Fatalf("passhash = %q, want %q", got, want)
	}
}
