package resources

import (
	"embed"
	"io/fs"
	"path"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

const (
	rbacRolePermissionsMountPath = "/opt/rbac/rbac/management/role/permissions"
	rbacRoleDefinitionsMountPath = "/opt/rbac/rbac/management/role/definitions"
	rbacRolePermissionsVolume    = "rbac-role-permissions"
	rbacRoleDefinitionsVolume    = "rbac-role-definitions"
)

//go:embed rbac-config/permissions/*
var rbacPermissionConfig embed.FS

//go:embed rbac-config/definitions/*
var rbacDefinitionConfig embed.FS

func embeddedConfigMapData(fsys embed.FS, root string) map[string]string {
	data := make(map[string]string)
	_ = fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, readErr := fsys.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		data[path.Base(p)] = string(b)
		return nil
	})
	return data
}

// RBACRolePermissionsConfigMap holds on-prem cost-management and sources permissions
// for manage.py seeds (same mount path as SaaS rbac-clowdapp).
func RBACRolePermissionsConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NameRBACRolePermissionsConfigMap(cfg),
			Namespace: cfg.Namespace,
			Labels:    Labels(cfg, "rbac-seed"),
		},
		Data: embeddedConfigMapData(rbacPermissionConfig, "rbac-config/permissions"),
	}
}

// RBACRoleDefinitionsConfigMap holds on-prem cost-management and sources role definitions
// for manage.py seeds.
func RBACRoleDefinitionsConfigMap(cfg *costv1alpha1.CostManagementServiceConfig) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NameRBACRoleDefinitionsConfigMap(cfg),
			Namespace: cfg.Namespace,
			Labels:    Labels(cfg, "rbac-seed"),
		},
		Data: embeddedConfigMapData(rbacDefinitionConfig, "rbac-config/definitions"),
	}
}

// appendRBACSeedConfigVolumes mounts operator-owned seed JSON over the image defaults.
func appendRBACSeedConfigVolumes(
	cfg *costv1alpha1.CostManagementServiceConfig,
	vols []corev1.Volume,
	mounts []corev1.VolumeMount,
) ([]corev1.Volume, []corev1.VolumeMount) {
	vols = append(vols,
		corev1.Volume{
			Name: rbacRolePermissionsVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: NameRBACRolePermissionsConfigMap(cfg)},
				},
			},
		},
		corev1.Volume{
			Name: rbacRoleDefinitionsVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: NameRBACRoleDefinitionsConfigMap(cfg)},
				},
			},
		},
	)
	mounts = append(mounts,
		corev1.VolumeMount{Name: rbacRolePermissionsVolume, MountPath: rbacRolePermissionsMountPath},
		corev1.VolumeMount{Name: rbacRoleDefinitionsVolume, MountPath: rbacRoleDefinitionsMountPath},
	)
	return vols, mounts
}
