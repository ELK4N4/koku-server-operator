package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

// validateObjectStorage checks S3 credentials then probes the object store.
// Non-blocking: failures set StorageReady=False but do not gate Migration.
//
// Secret resolution matches Discovery: user spec.objectStorage.secretName, else
// status.discoveredConfig.s3.secretName. Missing keys fail before any network
// call (G2). ListBuckets then confirms endpoint, TLS, and that the keys are
// accepted (G1).
func (r *CostManagementServiceConfigReconciler) validateObjectStorage(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) {
	secretName := cfg.Spec.ObjectStorage.SecretName
	if secretName == "" && cfg.Status.DiscoveredConfig != nil && cfg.Status.DiscoveredConfig.S3 != nil {
		secretName = cfg.Status.DiscoveredConfig.S3.SecretName
	}
	if secretName == "" {
		return
	}

	secret, err := r.checkSecretKeys(ctx, cfg.Namespace, secretName, s3SecretKeys)
	if err != nil {
		r.setCondition(cfg, costv1alpha1.ConditionStorageReady, metav1.ConditionFalse,
			"StorageSecretInvalid", err.Error())
		return
	}

	endpoint := resources.S3Endpoint(cfg)
	region := s3Region(cfg)
	accessKey := string(secret.Data[s3SecretKeys[0]])
	secretKey := string(secret.Data[s3SecretKeys[1]])
	if err := s3ListBucketsProbe(ctx, endpoint, region, accessKey, secretKey, validationTimeout, cfg.Spec.ObjectStorage.InsecureSkipVerify); err != nil {
		r.setCondition(cfg, costv1alpha1.ConditionStorageReady, metav1.ConditionFalse,
			"StorageUnreachable", err.Error())
		return
	}
	r.setCondition(cfg, costv1alpha1.ConditionStorageReady, metav1.ConditionTrue,
		"StorageReachable", fmt.Sprintf("ListBuckets %s", endpoint))
}

// s3ListBucketsProbe calls S3 ListBuckets against endpoint using path-style
// addressing (required for MinIO / NooBaa / Ceph RGW).
func s3ListBucketsProbe(ctx context.Context, endpoint, region, accessKey, secretKey string, timeout time.Duration, insecureSkipVerify bool) error {
	if region == "" {
		region = defaultS3Region
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse S3 endpoint %q: %w", endpoint, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("S3 endpoint %q must include scheme and host", endpoint)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify}, //nolint:gosec // user-controlled via CR spec
		},
	}
	client := s3.New(s3.Options{
		Region:       region,
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		HTTPClient:   httpClient,
		EndpointOptions: s3.EndpointResolverOptions{
			DisableHTTPS: u.Scheme == "http",
		},
	})
	if _, err := client.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
		return fmt.Errorf("ListBuckets %s: %w", endpoint, err)
	}
	return nil
}
