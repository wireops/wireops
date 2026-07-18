package remote

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

func init() {
	RegisterKMS("aws_kms", newAWSKeyManager)
}

// kmsAPI is the narrow subset of *kms.Client this package calls.
type kmsAPI interface {
	GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, opts ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error)
	Decrypt(ctx context.Context, in *kms.DecryptInput, opts ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

var _ kmsAPI = (*kms.Client)(nil)

type awsKeyManager struct {
	api   kmsAPI
	keyID string
}

// newAWSKeyManager reuses the same static access key/secret already
// configured for the S3 storage provider — wireops's remote-backup config
// is a single AWS credential set covering both S3 and KMS, not two separate
// ones. config carries kms_key_id and, optionally, kms_region (falling
// back to the storage provider's region if empty).
func newAWSKeyManager(config map[string]any, creds map[string]any) (KeyManager, error) {
	keyID := strFrom(config, "kms_key_id")
	region := strFrom(config, "kms_region")
	if region == "" {
		region = strFrom(config, "region")
	}
	accessKey := strFrom(creds, "access_key")
	secretKey := strFrom(creds, "secret_key")

	if keyID == "" || region == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("remote/kms: kms_key_id, region, access_key and secret_key are required")
	}

	client := kms.New(kms.Options{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	})

	return &awsKeyManager{api: client, keyID: keyID}, nil
}

func (m *awsKeyManager) GenerateDataKey(ctx context.Context) ([]byte, []byte, error) {
	out, err := m.api.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
		KeyId:   &m.keyID,
		KeySpec: types.DataKeySpecAes256,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("remote/kms: generate data key: %w", err)
	}
	return out.Plaintext, out.CiphertextBlob, nil
}

func (m *awsKeyManager) Decrypt(ctx context.Context, encrypted []byte) ([]byte, error) {
	out, err := m.api.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: encrypted,
		KeyId:          &m.keyID,
	})
	if err != nil {
		return nil, fmt.Errorf("remote/kms: decrypt data key: %w", err)
	}
	return out.Plaintext, nil
}
