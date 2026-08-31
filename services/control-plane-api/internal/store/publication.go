package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

var (
	ErrPublicationConflict         = errors.New("public endpoint is already allocated")
	ErrPublicationUnavailable      = errors.New("public TCP endpoint capacity is unavailable")
	ErrPublicationDomainNotAllowed = errors.New("publication domain is not allowed")
	ErrPublicationHostnameReserved = errors.New("publication hostname is reserved")
)

func NewPublicationPolicy(defaultDomain, statefulDomain string, tcpEnabled bool, minimumPort, maximumPort int32) PublicationPolicy {
	policy := PublicationPolicy{TCPEnabled: tcpEnabled, TCPMinimumPort: minimumPort, TCPMaximumPort: maximumPort}
	reserved := []string{"cloud"}
	if statefulDomain != "" {
		if child := strings.TrimSuffix(statefulDomain, "."+defaultDomain); child != statefulDomain && !strings.Contains(child, ".") {
			reserved = append(reserved, child)
		}
	}
	policy.Domains = append(policy.Domains, PublicationDomain{ID: "default", Suffix: defaultDomain, WorkloadKinds: []domain.WorkloadKind{domain.WorkloadStateless, domain.WorkloadStateful}, EndpointTypes: []string{domain.EndpointHTTP, domain.EndpointTCP}, ReservedLabels: reserved})
	if statefulDomain != "" {
		policy.Domains = append(policy.Domains, PublicationDomain{ID: "stateful", Suffix: statefulDomain, WorkloadKinds: []domain.WorkloadKind{domain.WorkloadStateful}, EndpointTypes: []string{domain.EndpointHTTP, domain.EndpointTCP}})
	}
	return policy
}

func (p PublicationPolicy) Resolve(workloadKind domain.WorkloadKind, endpoint domain.PublicEndpoint) (string, error) {
	for _, candidate := range p.Domains {
		if candidate.ID != endpoint.DomainID {
			continue
		}
		if !slices.Contains(candidate.WorkloadKinds, workloadKind) || !slices.Contains(candidate.EndpointTypes, endpoint.Type) {
			return "", ErrPublicationDomainNotAllowed
		}
		if slices.Contains(candidate.ReservedLabels, endpoint.HostnameLabel) {
			return "", ErrPublicationHostnameReserved
		}
		return endpoint.HostnameLabel + "." + candidate.Suffix, nil
	}
	return "", ErrPublicationDomainNotAllowed
}

func inheritAllocatedPorts(configuration, current domain.RuntimeConfig) domain.RuntimeConfig {
	for index := range configuration.PublicEndpoints {
		endpoint := &configuration.PublicEndpoints[index]
		if endpoint.Type != domain.EndpointTCP {
			continue
		}
		// External ports are allocated by the control plane. A client may echo an
		// existing value returned by the API, but it may never choose a new one.
		endpoint.ExternalPort = 0
		for _, existing := range current.PublicEndpoints {
			if existing.Type == endpoint.Type && existing.Name == endpoint.Name && existing.DomainID == endpoint.DomainID && existing.HostnameLabel == endpoint.HostnameLabel {
				endpoint.ExternalPort = existing.ExternalPort
				break
			}
		}
	}
	return configuration
}

func (s *Store) reservePublicationClaims(ctx context.Context, tx pgx.Tx, appEnvironmentID, configurationVersion int64, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig) (domain.RuntimeConfig, error) {
	for index := range configuration.PublicEndpoints {
		endpoint := &configuration.PublicEndpoints[index]
		hostname, err := s.Publication.Resolve(workloadKind, *endpoint)
		if err != nil {
			return domain.RuntimeConfig{}, err
		}
		var externalPort *int32
		if endpoint.Type == domain.EndpointTCP {
			if !s.Publication.TCPEnabled || s.Publication.TCPMinimumPort < 1 || s.Publication.TCPMaximumPort < s.Publication.TCPMinimumPort {
				return domain.RuntimeConfig{}, ErrPublicationUnavailable
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('molejo-public-tcp-pool'))`); err != nil {
				return domain.RuntimeConfig{}, err
			}
			allocated := endpoint.ExternalPort
			if allocated == 0 {
				_ = tx.QueryRow(ctx, `SELECT external_port FROM publication_claims WHERE app_environment_id=$1 AND endpoint_name=$2 AND endpoint_type='TCP' AND hostname=$3 ORDER BY current_configuration_version DESC NULLS LAST,desired_configuration_version DESC NULLS LAST LIMIT 1`, appEnvironmentID, endpoint.Name, hostname).Scan(&allocated)
			}
			if allocated == 0 {
				if err := tx.QueryRow(ctx, `SELECT candidate FROM generate_series($1::integer,$2::integer) candidate WHERE NOT EXISTS (SELECT 1 FROM publication_claims WHERE external_port=candidate) ORDER BY candidate LIMIT 1`, s.Publication.TCPMinimumPort, s.Publication.TCPMaximumPort).Scan(&allocated); errors.Is(err, pgx.ErrNoRows) {
					return domain.RuntimeConfig{}, ErrPublicationUnavailable
				} else if err != nil {
					return domain.RuntimeConfig{}, err
				}
			}
			endpoint.ExternalPort = allocated
			externalPort = &allocated
		}
		var claimID int64
		err = tx.QueryRow(ctx, `INSERT INTO publication_claims(app_environment_id,endpoint_name,endpoint_type,hostname,external_port,desired_configuration_version)
			VALUES($1,$2,$3,$4,$5,$6)
			ON CONFLICT(hostname) DO UPDATE SET endpoint_name=EXCLUDED.endpoint_name,endpoint_type=EXCLUDED.endpoint_type,external_port=EXCLUDED.external_port,desired_configuration_version=EXCLUDED.desired_configuration_version,updated_at=now()
			WHERE publication_claims.app_environment_id=EXCLUDED.app_environment_id
			RETURNING id`, appEnvironmentID, endpoint.Name, endpoint.Type, hostname, externalPort, configurationVersion).Scan(&claimID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) || uniqueConstraint(err) != "" {
				return domain.RuntimeConfig{}, ErrPublicationConflict
			}
			return domain.RuntimeConfig{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM publication_claims WHERE app_environment_id=$1 AND current_configuration_version IS NULL AND desired_configuration_version IS DISTINCT FROM $2`, appEnvironmentID, configurationVersion); err != nil {
		return domain.RuntimeConfig{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE publication_claims SET desired_configuration_version=NULL,updated_at=now() WHERE app_environment_id=$1 AND current_configuration_version IS NOT NULL AND desired_configuration_version IS DISTINCT FROM $2`, appEnvironmentID, configurationVersion); err != nil {
		return domain.RuntimeConfig{}, err
	}
	return configuration, nil
}

func activatePublicationClaims(ctx context.Context, tx pgx.Tx, appEnvironmentID, configurationVersion int64, workloadKind domain.WorkloadKind, configuration domain.RuntimeConfig, policy PublicationPolicy) error {
	for _, endpoint := range configuration.PublicEndpoints {
		hostname, err := policy.Resolve(workloadKind, endpoint)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE publication_claims SET current_configuration_version=$3,updated_at=now() WHERE app_environment_id=$1 AND hostname=$2`, appEnvironmentID, hostname, configurationVersion)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("activate publication claim: %w", ErrPublicationConflict)
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM publication_claims WHERE app_environment_id=$1 AND current_configuration_version IS DISTINCT FROM $2 AND desired_configuration_version IS NULL`, appEnvironmentID, configurationVersion)
	return err
}
