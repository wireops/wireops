package s3

// Config field keys, referenced by descriptor.Fields[i].Key in s3.go.
const (
	keyBucket         = "bucket"
	keyRegion         = "region"
	keyEndpoint       = "endpoint"
	keyPrefix         = "prefix"
	keyForcePathStyle = "force_path_style"
	keyAccessKey      = "access_key"
	keySecret         = "secret"
	keyKMSEnabled     = "kms_enabled"
	keyEncryptContent = "encrypt_content"
)
