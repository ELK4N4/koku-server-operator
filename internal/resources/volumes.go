package resources

import (
	corev1 "k8s.io/api/core/v1"

	costv1alpha1 "github.com/project-koku/koku-server-operator/api/v1alpha1"
)

// KokuVolumes returns the standard volume list shared by all Koku pods.
// Mirrors cost-onprem.koku.volumes in _helpers-koku.tpl.
func KokuVolumes(cfg *costv1alpha1.CostManagementServiceConfig) []corev1.Volume {
	vols := []corev1.Volume{
		{
			Name:         "tmp",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: "aws-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: NameAWSConfigMap(cfg)},
				},
			},
		},
		{
			Name: "ca-scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: NameCACombineConfigMap(cfg)},
					Items: []corev1.KeyToPath{
						{Key: "combine-ca.sh", Path: "combine-ca.sh", Mode: int32Ptr(0755)},
					},
				},
			},
		},
		{
			Name: "ca-source",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: NameServiceCAConfigMap(cfg)},
				},
			},
		},
		{
			Name:         "combined-ca-bundle",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}

	if cfg.Spec.Cache.TLS.Enabled && cfg.Spec.Cache.TLS.CACertSecretName != "" {
		vols = append(vols, corev1.Volume{
			Name: "redis-tls-ca",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.Spec.Cache.TLS.CACertSecretName,
					Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				},
			},
		})
	}

	// Kafka TLS CA (BYOI secured Kafka)
	if cfg.Spec.Kafka.TLS.Enabled && cfg.Spec.Kafka.TLS.CACertSecret != "" {
		vols = append(vols, corev1.Volume{
			Name: "kafka-ca-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cfg.Spec.Kafka.TLS.CACertSecret,
					Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				},
			},
		})
	}

	return vols
}

// KokuVolumeMounts returns the standard volume mounts for all Koku containers.
// Mirrors cost-onprem.koku.volumeMounts in _helpers-koku.tpl.
func KokuVolumeMounts(cfg *costv1alpha1.CostManagementServiceConfig) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "aws-config", MountPath: "/etc/aws", ReadOnly: true},
		{Name: "combined-ca-bundle", MountPath: "/etc/pki/ca-trust/combined", ReadOnly: true},
	}

	if cfg.Spec.Cache.TLS.Enabled && cfg.Spec.Cache.TLS.CACertSecretName != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "redis-tls-ca",
			MountPath: "/etc/redis-tls",
			ReadOnly:  true,
		})
	}

	// Kafka TLS CA (BYOI secured Kafka)
	if cfg.Spec.Kafka.TLS.Enabled && cfg.Spec.Kafka.TLS.CACertSecret != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "kafka-ca-cert",
			MountPath: "/etc/kafka/certs",
			ReadOnly:  true,
		})
	}

	return mounts
}

// CACombineInitContainer returns the init container that merges system and
// cluster CA certificates into a combined bundle.
func CACombineInitContainer(_ *costv1alpha1.CostManagementServiceConfig) corev1.Container {
	return corev1.Container{
		Name:    "prepare-ca-bundle",
		Image:   "registry.access.redhat.com/ubi9/ubi-minimal:9.7",
		Command: []string{"bash", "/scripts/combine-ca.sh"},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "ca-scripts", MountPath: "/scripts", ReadOnly: true},
			{Name: "ca-source", MountPath: "/ca-source", ReadOnly: true},
			{Name: "combined-ca-bundle", MountPath: "/ca-output"},
		},
		SecurityContext: restrictedContainerSC(),
	}
}

// WaitForValkeyInitContainer returns an init container that blocks until
// the Valkey service accepts connections.
func WaitForValkeyInitContainer(cfg *costv1alpha1.CostManagementServiceConfig) corev1.Container {
	host := CacheHost(cfg)
	port := "6379"
	if cfg.Spec.Cache.Port != 0 {
		port = int32String(cfg.Spec.Cache.Port)
	}
	return corev1.Container{
		Name:  "wait-for-valkey",
		Image: "registry.access.redhat.com/ubi9/ubi-minimal:9.7",
		// Use bash /dev/tcp — available in ubi-minimal without nc/ncat.
		Command: []string{
			"bash", "-c",
			`until bash -c "echo >/dev/tcp/` + host + `/` + port + `" 2>/dev/null; do echo 'waiting for valkey'; sleep 2; done`,
		},
		SecurityContext: restrictedContainerSC(),
	}
}

// kokuAppContainerSC is used for koku application containers (API, Masu,
// Listener, Celery). readOnlyRootFilesystem is omitted because koku's Django
// settings.py unconditionally configures a file log handler at
// /opt/koku/koku/app.log; Django instantiates all handler objects at startup
// regardless of DJANGO_LOG_HANDLERS, causing a boot failure on a read-only FS.
func kokuAppContainerSC() *corev1.SecurityContext {
	f := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &f,
		Privileged:               &f,
	}
}

// migrationContainerSC is like restrictedContainerSC but without
// readOnlyRootFilesystem, because Django's logging framework instantiates all
// configured handler objects at startup regardless of DJANGO_LOG_HANDLERS.
func migrationContainerSC() *corev1.SecurityContext {
	f := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &f,
		Privileged:               &f,
	}
}

func int32Ptr(i int32) *int32 { return &i }

func restrictedContainerSC() *corev1.SecurityContext {
	f := false
	t := true
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &f,
		Privileged:               &f,
		ReadOnlyRootFilesystem:   &t,
		RunAsNonRoot:             &t,
	}
}
