package v1alpha1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

const cmscCRDFile = "service.costmanagement.openshift.io_costmanagementserviceconfigs.yaml"

var saasCeleryWorkerFields = []string{"hcs", "subsExtraction", "subsTransmission"}

var crdInstallPaths = []struct {
	name string
	path string
}{
	{name: "config CRD", path: filepath.Join("..", "..", "config", "crd", "bases", cmscCRDFile)},
	{name: "OLM bundle CRD", path: filepath.Join("..", "..", "bundle", "manifests", cmscCRDFile)},
}

// TestSaaSCeleryWorkerCRDDefaultReplicas locks the apiserver defaulting contract
// for SaaS-only Celery queues. Builder unit tests only see in-memory Go zero
// values; OLM installs read bundle/manifests, not config/crd alone.
func TestSaaSCeleryWorkerCRDDefaultReplicas(t *testing.T) {
	t.Parallel()

	for _, install := range crdInstallPaths {
		t.Run(install.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(install.path)
			if err != nil {
				t.Fatalf("read CRD %s: %v", install.path, err)
			}

			var crd apiextensionsv1.CustomResourceDefinition
			if err := yaml.Unmarshal(data, &crd); err != nil {
				t.Fatalf("unmarshal CRD: %v", err)
			}

			schema := storedOpenAPIV3Schema(&crd)
			if schema == nil {
				t.Fatal("missing openAPIV3Schema on stored CRD version")
			}

			workers := navigateSchema(t, schema, "spec", "costManagement", "celery", "workers")
			for _, field := range saasCeleryWorkerFields {
				if got := replicasDefault(t, workers, field); got != 0 {
					t.Errorf("workers.%s.replicas default = %d, want 0", field, got)
				}
			}
		})
	}
}

func storedOpenAPIV3Schema(crd *apiextensionsv1.CustomResourceDefinition) *apiextensionsv1.JSONSchemaProps {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Storage {
			return crd.Spec.Versions[i].Schema.OpenAPIV3Schema
		}
	}
	if len(crd.Spec.Versions) == 0 {
		return nil
	}
	return crd.Spec.Versions[0].Schema.OpenAPIV3Schema
}

func navigateSchema(t *testing.T, root *apiextensionsv1.JSONSchemaProps, path ...string) *apiextensionsv1.JSONSchemaProps {
	t.Helper()

	cur := root
	for _, seg := range path {
		if cur.Properties == nil {
			t.Fatalf("missing properties while navigating to %q", seg)
		}
		next, ok := cur.Properties[seg]
		if !ok {
			t.Fatalf("missing schema property %q", seg)
		}
		cur = &next
	}
	return cur
}

func replicasDefault(t *testing.T, workers *apiextensionsv1.JSONSchemaProps, queue string) int64 {
	t.Helper()

	replicasSchema := navigateSchema(t, navigateSchema(t, workers, queue), "replicas")
	if replicasSchema.Default == nil {
		t.Fatalf("workers.%s.replicas missing OpenAPI default", queue)
	}

	var asInt int64
	if err := json.Unmarshal(replicasSchema.Default.Raw, &asInt); err == nil {
		return asInt
	}

	var asFloat float64
	if err := json.Unmarshal(replicasSchema.Default.Raw, &asFloat); err != nil {
		t.Fatalf("workers.%s.replicas default is not numeric: %v", queue, err)
	}
	return int64(asFloat)
}
