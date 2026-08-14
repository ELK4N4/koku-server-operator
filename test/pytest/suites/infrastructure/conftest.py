"""
Infrastructure suite fixtures.

Fixtures for database, S3, and Kafka testing.
"""

import pytest

from utils import run_oc_command, get_secret_value, exec_in_pod


@pytest.fixture(scope="module")
def kafka_namespace(cluster_config) -> str:
    """Get the Kafka namespace."""
    # Try to find Kafka in common namespaces
    for ns in ["kafka", cluster_config.namespace, "strimzi"]:
        result = run_oc_command([
            "get", "kafka", "-n", ns,
            "-o", "jsonpath={.items[0].metadata.name}"
        ], check=False)
        if result.stdout.strip():
            return ns
    
    return "kafka"  # Default


@pytest.fixture(scope="module")
def kafka_cluster_name(kafka_namespace: str) -> str:
    """Get the Kafka cluster name."""
    result = run_oc_command([
        "get", "kafka", "-n", kafka_namespace,
        "-o", "jsonpath={.items[0].metadata.name}"
    ], check=False)
    
    name = result.stdout.strip()
    return name if name else "cost-onprem-kafka"


@pytest.fixture(scope="module")
def kafka_bootstrap_servers(kafka_namespace: str, kafka_cluster_name: str) -> str:
    """Get Kafka bootstrap servers."""
    return f"{kafka_cluster_name}-kafka-bootstrap.{kafka_namespace}.svc:9092"
