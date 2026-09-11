package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

var ErrPublicationDependency = errors.New("publication_has_dependents")

type AdministrativeDomain struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Kind          domain.PublicationDomainKind `json:"kind"`
	ReservedNames []string                     `json:"reservedNames"`
	Version       int64                        `json:"version"`
	CreatedAt     time.Time                    `json:"createdAt"`
	UpdatedAt     time.Time                    `json:"updatedAt"`
}
type PublicationOption struct {
	Domain     AdministrativeDomain             `json:"domain"`
	BindingID  string                           `json:"bindingId"`
	Listeners  []kubernetesbinding.HTTPListener `json:"listeners"`
	Health     kubernetesbinding.Health         `json:"health"`
	ReasonCode string                           `json:"reasonCode"`
}
type PublicationDependent struct {
	AppEnvironmentID string `json:"appEnvironmentId"`
	Hostname         string `json:"hostname"`
	Kind             string `json:"kind"`
}

// Hostnames are globally unique in this alpha. A single transaction lock keeps
// administrative revocation and set reservation atomic, including absent rows.
func lockPublication(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('molejo-http-publication'))`)
	return err
}

func scanDomain(row pgx.Row) (AdministrativeDomain, error) {
	var d AdministrativeDomain
	var raw []byte
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &raw, &d.Version, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &d.ReservedNames)
	}
	return d, err
}

const domainColumns = `id,name,kind,reserved_names,version,created_at,updated_at`

func (s *Store) PublicationDomains(ctx context.Context, offset int) ([]AdministrativeDomain, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+domainColumns+` FROM publication_domains ORDER BY id LIMIT 101 OFFSET $1`, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AdministrativeDomain{}
	for rows.Next() {
		d, e := scanDomain(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) PutPublicationDomain(ctx context.Context, id string, input AdministrativeDomain, actor int64, expected *int64, event audit.Event) (AdministrativeDomain, error) {
	if len(id) > 128 || !runtimeObjectNamePattern.MatchString(id) {
		return AdministrativeDomain{}, domain.ErrPublicationName
	}
	name, err := domain.NormalizePublicationName(input.Name)
	if err != nil {
		return AdministrativeDomain{}, err
	}
	if input.Kind != domain.PublicationExact && input.Kind != domain.PublicationPool {
		return AdministrativeDomain{}, domain.ErrPublicationUnsupported
	}
	if len(input.ReservedNames) > 100 {
		return AdministrativeDomain{}, domain.ErrPublicationLimit
	}
	input.ReservedNames = append([]string{}, input.ReservedNames...)
	for i, n := range input.ReservedNames {
		input.ReservedNames[i], err = domain.NormalizePublicationName(n)
		if err != nil {
			return AdministrativeDomain{}, err
		}
	}
	sort.Strings(input.ReservedNames)
	input.ReservedNames = slices.Compact(input.ReservedNames)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return AdministrativeDomain{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return AdministrativeDomain{}, err
	}
	raw, _ := json.Marshal(input.ReservedNames)
	if expected == nil {
		existing, e := scanDomain(tx.QueryRow(ctx, `SELECT `+domainColumns+` FROM publication_domains WHERE id=$1`, id))
		if e == nil {
			if existing.Name == name && existing.Kind == input.Kind && slices.Equal(existing.ReservedNames, input.ReservedNames) {
				return existing, tx.Commit(ctx)
			}
			return existing, ErrConflict
		}
		if !errors.Is(e, ErrNotFound) {
			return existing, e
		}
		_, err = tx.Exec(ctx, `INSERT INTO publication_domains(id,name,kind,reserved_names,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$5)`, id, name, input.Kind, raw, actor)
	} else {
		existing, e := scanDomain(tx.QueryRow(ctx, `SELECT `+domainColumns+` FROM publication_domains WHERE id=$1`, id))
		if e != nil {
			return existing, e
		}
		if existing.Version != *expected {
			return existing, ErrVersionConflict
		}
		if existing.Name == name && existing.Kind == input.Kind && slices.Equal(existing.ReservedNames, input.ReservedNames) {
			return existing, tx.Commit(ctx)
		}
		// Reject only edits that invalidate a protected name. Unrelated pool
		// reservations may be added without interrupting existing applications.
		var incompatible bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publication_claims c WHERE c.domain_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM unnest($3::text[]) reserved(name) WHERE c.hostname=reserved.name OR right(c.hostname,length(reserved.name)+1)='.'||reserved.name)))`, id, existing.Name != name || existing.Kind != input.Kind, input.ReservedNames).Scan(&incompatible); e != nil {
			return existing, e
		}
		if incompatible {
			return existing, ErrPublicationDependency
		}
		tag, e := tx.Exec(ctx, `UPDATE publication_domains SET name=$2,kind=$3,reserved_names=$4,version=version+1,updated_by=$5,updated_at=now() WHERE id=$1 AND version=$6`, id, name, input.Kind, raw, actor, *expected)
		err = e
		if err == nil && tag.RowsAffected() != 1 {
			err = ErrVersionConflict
		}
	}
	if err != nil {
		return AdministrativeDomain{}, translateDBError(err)
	}
	event.ActorUserID = &actor
	event.TargetPublicID = id
	if err = insertAudit(ctx, tx, event); err != nil {
		return AdministrativeDomain{}, err
	}
	d, err := scanDomain(tx.QueryRow(ctx, `SELECT `+domainColumns+` FROM publication_domains WHERE id=$1`, id))
	if err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (s *Store) DeletePublicationDomain(ctx context.Context, id string, actor, version int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	var used bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publication_grants WHERE domain_id=$1)`, id).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return ErrPublicationDependency
	}
	tag, err := tx.Exec(ctx, `DELETE FROM publication_domains WHERE id=$1 AND version=$2`, id, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrVersionConflict
	}
	event.ActorUserID = &actor
	event.TargetPublicID = id
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetPublicationGrant(ctx context.Context, domainID, workspaceID, bindingID string, actor int64, remove bool, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	var wid int64
	if err = tx.QueryRow(ctx, `SELECT w.id FROM workspaces w JOIN workspace_clusters wc ON wc.workspace_id=w.id JOIN cluster_publication_bindings b ON b.cluster_id=wc.installation_id JOIN publication_domains d ON d.id=$3 WHERE w.public_id=$1 AND b.id=$2`, workspaceID, bindingID, domainID).Scan(&wid); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if remove {
		var used bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publication_claims c JOIN app_environments ae ON ae.id=c.app_environment_id WHERE c.domain_id=$1 AND c.binding_id=$2 AND ae.workspace_id=$3)`, domainID, bindingID, wid).Scan(&used)
		if err != nil {
			return err
		}
		if used {
			return ErrPublicationDependency
		}
		_, err = tx.Exec(ctx, `DELETE FROM publication_grants WHERE domain_id=$1 AND workspace_id=$2 AND binding_id=$3`, domainID, wid, bindingID)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO publication_grants(domain_id,workspace_id,binding_id,created_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, domainID, wid, bindingID, actor)
	}
	if err != nil {
		return translateDBError(err)
	}
	event.ActorUserID = &actor
	event.TargetPublicID = domainID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func publicationDependents(ctx context.Context, tx pgx.Tx, domainID, bindingID string, offset ...int) ([]PublicationDependent, error) {
	skip := 0
	if len(offset) > 0 {
		skip = offset[0]
	}
	rows, err := tx.Query(ctx, `WITH refs AS (
 SELECT ae.public_id,c.hostname,c.desired_configuration_version,c.current_configuration_version,
 EXISTS(SELECT 1 FROM publication_execution_claims ec JOIN deployments d ON d.id=ec.deployment_id WHERE d.app_environment_id=ae.id AND ec.hostname=c.hostname) executable
 FROM publication_claims c JOIN app_environments ae ON ae.id=c.app_environment_id
 WHERE ($1='' OR c.domain_id=$1) AND ($2='' OR c.binding_id=$2)
 ) SELECT public_id,hostname,kind FROM refs CROSS JOIN LATERAL (
 SELECT 'Desired' kind WHERE desired_configuration_version IS NOT NULL
 UNION ALL SELECT 'Applied' WHERE current_configuration_version IS NOT NULL
 UNION ALL SELECT 'Executable' WHERE executable) kinds ORDER BY public_id,hostname,kind LIMIT 101 OFFSET $3`, domainID, bindingID, skip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PublicationDependent{}
	for rows.Next() {
		var d PublicationDependent
		if err = rows.Scan(&d.AppEnvironmentID, &d.Hostname, &d.Kind); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) PublicationDependents(ctx context.Context, domainID, bindingID string, offset int) ([]PublicationDependent, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	return publicationDependents(ctx, tx, domainID, bindingID, offset)
}

func (s *Store) PublicationDomain(ctx context.Context, id string) (AdministrativeDomain, error) {
	return scanDomain(s.Pool.QueryRow(ctx, `SELECT `+domainColumns+` FROM publication_domains WHERE id=$1`, id))
}

type PublicationGrant struct {
	DomainID    string    `json:"domainId"`
	WorkspaceID string    `json:"workspaceId"`
	BindingID   string    `json:"bindingId"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) PublicationGrants(ctx context.Context, id string, offset int) ([]PublicationGrant, error) {
	rows, err := s.Pool.Query(ctx, `SELECT g.domain_id,w.public_id,g.binding_id,g.created_at FROM publication_grants g JOIN workspaces w ON w.id=g.workspace_id WHERE g.domain_id=$1 ORDER BY w.public_id,g.binding_id LIMIT 101 OFFSET $2`, id, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PublicationGrant{}
	for rows.Next() {
		var item PublicationGrant
		if err = rows.Scan(&item.DomainID, &item.WorkspaceID, &item.BindingID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
