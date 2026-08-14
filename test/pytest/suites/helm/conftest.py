"""
Helm suite fixtures.
"""

import os
from pathlib import Path

import pytest


@pytest.fixture(scope="module")
def chart_path(cluster_config) -> str:
    """Get the path to the Helm chart directory."""
    # Try to find chart relative to project root
    project_root = cluster_config.project_root
    chart_dir = Path(project_root) / "cost-onprem"
    
    if chart_dir.exists():
        return str(chart_dir)
    
    # Fallback: look for Chart.yaml in parent directories
    current = Path(__file__).parent
    for _ in range(5):
        candidate = current / "cost-onprem"
        if candidate.exists() and (candidate / "Chart.yaml").exists():
            return str(candidate)
        current = current.parent
    
    pytest.skip("Helm chart directory not found")


@pytest.fixture(scope="module")
def values_file(chart_path: str) -> str:
    """Get the path to the default values.yaml."""
    values_path = Path(chart_path) / "values.yaml"
    if not values_path.exists():
        pytest.skip("values.yaml not found")
    return str(values_path)


@pytest.fixture(scope="module")
def openshift_values_file(cluster_config) -> str:
    """Get the path to openshift-values.yaml."""
    project_root = cluster_config.project_root
    values_path = Path(project_root) / "openshift-values.yaml"
    if not values_path.exists():
        pytest.skip("openshift-values.yaml not found")
    return str(values_path)
