# ADR-0006: Control Plane Topology and Runtime Boundary

Status: Draft

## Context

The first Fruto beta needs a small product API and web console without making
Kubernetes the public API. PostgreSQL stores product intent and history while
the existing platform operator owns runtime children.

## Decision

The control plane is a single-replica Go service with a React/Vite console and a
PostgreSQL dependency. It applies only `AppDeployment` resources through a
consumer-owned runtime interface. The operator remains the sole owner of
Deployments, Services, and HTTPRoutes. The first installation uses one shared
Workspace Namespace and explicit kubeconfig or in-cluster credentials.

This is a pre-alpha, non-HA topology. The API exposes sanitized product
resources and never returns Kubernetes metadata or raw objects.

## Consequences

The boundary is portable to a future management cluster, but this increment does
not provide remote cluster discovery, HA, or production disaster recovery.
