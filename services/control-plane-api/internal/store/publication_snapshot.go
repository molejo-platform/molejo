package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

type ResolvedPublication struct {
	EndpointName string                            `json:"endpointName"`
	DomainID     string                            `json:"domainId"`
	Hostname     string                            `json:"hostname"`
	Destination  kubernetesbinding.HTTPDestination `json:"destination"`
}
type PublicationSnapshot struct {
	SchemaVersion string                `json:"schemaVersion"`
	Addresses     []ResolvedPublication `json:"addresses"`
}

func (s *Store) resolveHTTPConfiguration(ctx context.Context, tx pgx.Tx, environmentID int64, config domain.RuntimeConfig) (domain.RuntimeConfig, PublicationSnapshot, error) {
	rawConfig, err := json.Marshal(config)
	if err != nil {
		return config, PublicationSnapshot{}, err
	}
	var resolved domain.RuntimeConfig
	if err = json.Unmarshal(rawConfig, &resolved); err != nil {
		return config, PublicationSnapshot{}, err
	}
	config = resolved
	snapshot := PublicationSnapshot{SchemaVersion: "publication.v1", Addresses: []ResolvedPublication{}}
	seen := map[string]bool{}
	for ei := range config.PublicEndpoints {
		endpoint := &config.PublicEndpoints[ei]
		if endpoint.Type != domain.EndpointHTTP {
			continue
		}
		if len(endpoint.Addresses) < 1 || len(endpoint.Addresses) > 10 || endpoint.DomainID != "" || endpoint.HostnameLabel != "" {
			return config, snapshot, domain.ErrPublicationName
		}
		for ai := range endpoint.Addresses {
			a := &endpoint.Addresses[ai]
			var d domain.PublicationDomain
			var reservations, raw []byte
			var b kubernetesbinding.HTTPBinding
			err := tx.QueryRow(ctx, `SELECT d.id,d.name,d.kind,d.reserved_names,b.id,b.version,b.configuration FROM publication_domains d JOIN publication_grants g ON g.domain_id=d.id JOIN cluster_publication_bindings b ON b.id=g.binding_id JOIN app_environments ae ON ae.workspace_id=g.workspace_id AND ae.cluster_id=b.cluster_id WHERE ae.id=$1 AND d.id=$2 AND b.id=$3`, environmentID, a.DomainID, a.BindingID).Scan(&d.ID, &d.Name, &d.Kind, &reservations, &b.ID, &b.Revision, &raw)
			if errors.Is(err, pgx.ErrNoRows) {
				return config, snapshot, domain.ErrPublicationNotGranted
			}
			if err != nil {
				return config, snapshot, err
			}
			if err = json.Unmarshal(reservations, &d.ReservedNames); err != nil {
				return config, snapshot, err
			}
			id, revision := b.ID, b.Revision
			if err = json.Unmarshal(raw, &b); err != nil {
				return config, snapshot, err
			}
			b.ID, b.Revision = id, revision
			hostname, err := d.Resolve(a.Label)
			if err != nil {
				return config, snapshot, err
			}
			if seen[hostname] {
				return config, snapshot, domain.ErrPublicationName
			}
			seen[hostname] = true
			destination, err := b.Resolve(hostname, a.ListenerName)
			if err != nil {
				return config, snapshot, err
			}
			a.Hostname, a.ListenerName = hostname, destination.SectionName
			snapshot.Addresses = append(snapshot.Addresses, ResolvedPublication{EndpointName: endpoint.Name, DomainID: d.ID, Hostname: hostname, Destination: destination})
		}
	}
	return config, snapshot, nil
}

func reserveHTTPClaim(ctx context.Context, tx pgx.Tx, environmentID, version int64, a ResolvedPublication, desired bool) error {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO publication_claims(app_environment_id,endpoint_name,endpoint_type,hostname,domain_id,binding_id,desired_configuration_version)
 VALUES($1,$2,'HTTP',$3,$4,$5,CASE WHEN $7 THEN $6::bigint ELSE NULL END)
 ON CONFLICT(hostname) DO UPDATE SET endpoint_name=CASE WHEN $7 THEN EXCLUDED.endpoint_name ELSE publication_claims.endpoint_name END,desired_configuration_version=CASE WHEN $7 THEN EXCLUDED.desired_configuration_version ELSE publication_claims.desired_configuration_version END,updated_at=now()
 WHERE publication_claims.app_environment_id=$1 AND publication_claims.domain_id=$4 AND publication_claims.binding_id=$5 RETURNING id`, environmentID, a.EndpointName, a.Hostname, a.DomainID, a.Destination.BindingID, version, desired).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || uniqueConstraint(err) != "" {
		return ErrPublicationConflict
	}
	return err
}

// A saved revision does not replace deployment reservations. References remain
// live until a newer correlated projection or terminal withdrawal fences them.
func cleanupPublicationClaims(ctx context.Context, tx pgx.Tx, environmentID int64) error {
	_, err := tx.Exec(ctx, `DELETE FROM publication_claims c WHERE app_environment_id=$1 AND desired_configuration_version IS NULL AND current_configuration_version IS NULL AND NOT EXISTS(SELECT 1 FROM publication_execution_claims ec JOIN deployments d ON d.id=ec.deployment_id WHERE d.app_environment_id=c.app_environment_id AND ec.hostname=c.hostname)`, environmentID)
	return err
}

func (s *Store) PublicationSnapshotForDeployment(ctx context.Context, deploymentID int64) (PublicationSnapshot, error) {
	var snapshot PublicationSnapshot
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT publication_snapshot FROM deployments WHERE id=$1`, deploymentID).Scan(&raw)
	if err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.SchemaVersion != "publication.v1" {
		return snapshot, errors.New("publication_snapshot_incompatible")
	}
	for _, a := range snapshot.Addresses {
		var config []byte
		var id string
		var revision int64
		err = s.Pool.QueryRow(ctx, `SELECT b.id,b.version,b.configuration FROM deployments d JOIN publication_grants g ON g.workspace_id=d.workspace_id JOIN cluster_publication_bindings b ON b.id=g.binding_id JOIN app_environments ae ON ae.id=d.app_environment_id AND ae.cluster_id=b.cluster_id WHERE d.id=$1 AND g.domain_id=$2 AND b.id=$3`, deploymentID, a.DomainID, a.Destination.BindingID).Scan(&id, &revision, &config)
		if errors.Is(err, pgx.ErrNoRows) {
			return snapshot, domain.ErrPublicationNotGranted
		}
		if err != nil {
			return snapshot, err
		}
		var b kubernetesbinding.HTTPBinding
		if err = json.Unmarshal(config, &b); err != nil {
			return snapshot, err
		}
		b.ID, b.Revision = id, revision
		if !b.SupportsSnapshot(a.Destination, a.Hostname) {
			return snapshot, ErrBindingUnsupported
		}
	}
	return snapshot, nil
}

func (s *Store) PublicationOptions(ctx context.Context, workspaceID int64, clusterID string, offset int) ([]PublicationOption, error) {
	rows, err := s.Pool.Query(ctx, `SELECT d.id,d.name,d.kind,d.reserved_names,d.version,d.created_at,d.updated_at,b.id,b.configuration,b.observation,b.expires_at FROM publication_grants g JOIN publication_domains d ON d.id=g.domain_id JOIN cluster_publication_bindings b ON b.id=g.binding_id JOIN agent_installations i ON i.id=b.cluster_id JOIN workspace_clusters wc ON wc.workspace_id=g.workspace_id AND wc.installation_id=b.cluster_id WHERE g.workspace_id=$1 AND i.public_id=$2 ORDER BY d.id,b.id LIMIT 101 OFFSET $3`, workspaceID, clusterID, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PublicationOption{}
	for rows.Next() {
		var o PublicationOption
		var reservations, config, observation []byte
		var expires *time.Time
		if err = rows.Scan(&o.Domain.ID, &o.Domain.Name, &o.Domain.Kind, &reservations, &o.Domain.Version, &o.Domain.CreatedAt, &o.Domain.UpdatedAt, &o.BindingID, &config, &observation, &expires); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(reservations, &o.Domain.ReservedNames); err != nil {
			return nil, err
		}
		var b kubernetesbinding.HTTPBinding
		if err = json.Unmarshal(config, &b); err != nil {
			return nil, err
		}
		o.Listeners = b.Listeners
		o.Health, o.ReasonCode = publicationBindingHealth(observation, b.Listeners, time.Now())
		result = append(result, o)
	}
	return result, rows.Err()
}
