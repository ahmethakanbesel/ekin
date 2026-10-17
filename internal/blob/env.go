package blob

import (
	"errors"
	"path/filepath"
)

// FromEnv opens the store the EKIN_BLOB_* and EKIN_S3_* variables name
// (as internal/config reads them), for processes started without the
// configuration, such as the agent's tools. The fs driver needs an
// absolute EKIN_BLOB_DIR: the process may run in another directory.
func FromEnv(getenv func(string) string) (Store, error) {
	switch getenv("EKIN_BLOB_DRIVER") {
	case "s3":
		region := getenv("EKIN_S3_REGION")
		if region == "" {
			region = "auto"
		}
		return NewS3(S3Config{
			Endpoint: getenv("EKIN_S3_ENDPOINT"), Region: region, Bucket: getenv("EKIN_S3_BUCKET"),
			Prefix: getenv("EKIN_S3_PREFIX"), AccessKey: getenv("EKIN_S3_ACCESS_KEY_ID"),
			SecretKey: getenv("EKIN_S3_SECRET_ACCESS_KEY"),
		})
	case "fs":
		dir := getenv("EKIN_BLOB_DIR")
		if !filepath.IsAbs(dir) {
			return nil, errors.New("blob: EKIN_BLOB_DIR must be an absolute path")
		}
		return NewFS(dir)
	}
	return nil, errors.New("blob: EKIN_BLOB_DRIVER is not set")
}
