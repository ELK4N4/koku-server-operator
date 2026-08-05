# CRD Naming Comparison

## Selected naming vs sibling operator

|                 | koku-metrics-operator                                    | koku-server-operator                                      |
|-----------------|----------------------------------------------------------|-----------------------------------------------------------|
| Domain          | `openshift.io`                                           | `openshift.io` ✓                                          |
| Group           | `costmanagement-metrics-cfg`                             | `costmanagement-service-cfg` ✓                            |
| Kind            | `CostManagementMetricsConfig`                            | `CostManagementServiceConfig` ✓                           |
| Full API group  | `costmanagement-metrics-cfg.openshift.io`                | `costmanagement-service-cfg.openshift.io` ✓               |
| CRD name        | `costmanagementmetricsconfigs.costmanagement-metrics-cfg.openshift.io` | `costmanagementserviceconfigs.costmanagement-service-cfg.openshift.io` ✓ |
| apiVersion      | `costmanagement-metrics-cfg.openshift.io/v1beta1`        | `costmanagement-service-cfg.openshift.io/v1alpha1` ✓      |

## Scaffold command

```bash
operator-sdk init --domain openshift.io --repo github.com/project-koku/koku-server-operator
operator-sdk create api --group costmanagement-service-cfg --version v1alpha1 --kind CostManagementServiceConfig --resource --controller
```

## PROJECT file

```yaml
domain: openshift.io
group: costmanagement-service-cfg
kind: CostManagementServiceConfig
version: v1alpha1
```
