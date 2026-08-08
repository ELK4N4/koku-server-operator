package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

func uiTestCfg() *costv1alpha1.CostManagementServiceConfig {
	cfg := testCfg()
	cfg.Status.DiscoveredConfig = &costv1alpha1.DiscoveredConfig{ClusterDomain: "apps.example.com"}
	cfg.Spec.UI = costv1alpha1.UIConfig{
		ReplicaCount: 1,
		OAuthProxy: costv1alpha1.OAuthProxySpec{
			Image: costv1alpha1.ImageSpec{
				Repository: "registry.redhat.io/rhceph/oauth2-proxy-rhel9",
				Tag:        "v7.6.0",
			},
			CookieExpire: "720h",
		},
		App: costv1alpha1.UIAppSpec{
			Image: costv1alpha1.ImageSpec{
				Repository: "quay.io/insights-onprem/koku-ui-onprem",
				Tag:        "2f23c646581028bd385856b6713e6bf367baf953",
			},
		},
	}
	return cfg
}

func TestUIDeploymentOAuthProxyHasNumericUID(t *testing.T) {
	dep := UIDeployment(uiTestCfg())
	proxy := containerByName(t, dep.Spec.Template.Spec.Containers, "oauth-proxy")
	sc := proxy.SecurityContext
	if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser == 0 {
		t.Fatalf("oauth-proxy SecurityContext.RunAsUser = %v; want non-zero numeric UID (image runs as root)", sc)
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("oauth-proxy must set runAsNonRoot=true")
	}
}

func TestUIDeploymentAppHasWritableNginxPaths(t *testing.T) {
	dep := UIDeployment(uiTestCfg())
	app := containerByName(t, dep.Spec.Template.Spec.Containers, "app")

	mounts := map[string]string{}
	for _, m := range app.VolumeMounts {
		mounts[m.MountPath] = m.Name
	}
	for _, path := range []string{"/var/lib/nginx/tmp", "/var/log/nginx", "/tmp", "/run"} {
		if mounts[path] == "" {
			t.Errorf("app missing VolumeMount for %s (needed with readOnlyRootFilesystem)", path)
		}
	}

	vols := map[string]bool{}
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.EmptyDir != nil {
			vols[v.Name] = true
		}
	}
	for _, name := range []string{mounts["/var/lib/nginx/tmp"], mounts["/var/log/nginx"], mounts["/tmp"]} {
		if name == "" {
			continue
		}
		if !vols[name] {
			t.Errorf("volume %q for nginx writable path is not emptyDir", name)
		}
	}

	sc := app.SecurityContext
	if sc == nil || sc.RunAsUser == nil || *sc.RunAsUser != ubiMinimalNonRootUID {
		t.Errorf("app RunAsUser = %v; want %d (koku-ui-onprem USER)", sc, ubiMinimalNonRootUID)
	}
}

func containerByName(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for i := range containers {
		if containers[i].Name == name {
			return containers[i]
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}
