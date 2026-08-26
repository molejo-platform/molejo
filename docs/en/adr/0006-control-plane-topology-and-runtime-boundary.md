# ADR-0006: Control Plane Topology and Runtime Boundary

Status: Draft

## Context

The first Molejo beta needs a small product API and web console without making
Kubernetes the public API. PostgreSQL stores product intent and history while
the existing platform operator owns runtime children.

## Decision

The control plane is a single-replica Go service with a React/Vite console and a
PostgreSQL dependency. It applies only `AppDeployment` resources through a
consumer-owned runtime interface. The operator remains the sole owner of
Deployments, Services, and HTTPRoutes. Actors may belong to multiple Workspaces;
each Workspace contains Projects, while Apps and Environments are siblings under
one Project. An AppDeployment binds exactly one App to one Environment in that
same Project. Each Workspace is materialized as a managed Namespace through a
durable operation, using explicit kubeconfig or in-cluster credentials.

This is a pre-alpha, non-HA topology. The API exposes sanitized product
resources and never returns Kubernetes metadata or raw objects.

## Consequences

The boundary is portable to a future management cluster, but this increment does
not provide remote cluster discovery, HA, or production disaster recovery.
