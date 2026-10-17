package app

import (
	"path/filepath"
	"slices"
	"testing"

	"kurgu/internal/config"
)

// The tool runs in a draft's work dir, so a relative blob directory must
// reach it absolute.
func TestLibraryEnv(t *testing.T) {
	env, err := libraryEnv(config.Blob{Driver: "fs", Dir: "data/blobs"})
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("data/blobs")
	if !slices.Equal(env, []string{"KURGU_BLOB_DRIVER=fs", "KURGU_BLOB_DIR=" + abs}) {
		t.Fatalf("fs env = %v", env)
	}
	env, _ = libraryEnv(config.Blob{Driver: "s3", S3: config.S3{Endpoint: "https://s3.example.com", Bucket: "b"}})
	if !slices.Contains(env, "KURGU_BLOB_DRIVER=s3") || !slices.Contains(env, "KURGU_S3_BUCKET=b") {
		t.Fatalf("s3 env = %v", env)
	}
}
