package controlplane

import (
	"context"
	"net/http"

	controlplanev1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/controlplane/v1alpha1"
)

func (c *Client) Cluster(ctx context.Context, id string) (controlplanev1alpha1.Cluster, error) {
	response, err := c.api.GetCluster(ctx, id)
	if err != nil {
		return controlplanev1alpha1.Cluster{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return controlplanev1alpha1.Cluster{}, responseError(response)
	}
	var result controlplanev1alpha1.Cluster
	err = decode(response, &result)
	return result, err
}

func (c *Client) PublicationBinding(ctx context.Context, clusterID string) (*controlplanev1alpha1.ClusterPublicationBinding, error) {
	response, err := c.api.GetClusterPublicationBinding(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}
	var result controlplanev1alpha1.ClusterPublicationBinding
	err = decode(response, &result)
	return &result, err
}

func (c *Client) PublicationDomain(ctx context.Context, id string) (*controlplanev1alpha1.PublicationDomain, error) {
	response, err := c.api.GetPublicationDomain(ctx, id)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}
	var result controlplanev1alpha1.PublicationDomain
	err = decode(response, &result)
	return &result, err
}

func (c *Client) PublicationGrant(ctx context.Context, domainID, workspaceID, bindingID string) (*controlplanev1alpha1.PublicationGrant, error) {
	response, err := c.api.GetPublicationGrant(ctx, domainID, workspaceID, bindingID)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, responseError(response)
	}
	var result controlplanev1alpha1.PublicationGrant
	err = decode(response, &result)
	return &result, err
}

func (c *Client) PutPublicationBinding(ctx context.Context, clusterID string, expected *int, input controlplanev1alpha1.ClusterPublicationBindingInput) (controlplanev1alpha1.ClusterPublicationBinding, error) {
	response, err := c.api.PutClusterPublicationBinding(ctx, clusterID, &controlplanev1alpha1.PutClusterPublicationBindingParams{IfMatch: expected}, input)
	if err != nil {
		return controlplanev1alpha1.ClusterPublicationBinding{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return controlplanev1alpha1.ClusterPublicationBinding{}, responseError(response)
	}
	var result controlplanev1alpha1.ClusterPublicationBinding
	err = decode(response, &result)
	return result, err
}

func (c *Client) PutPublicationDomain(ctx context.Context, id string, expected *int, input controlplanev1alpha1.PublicationDomainInput) (controlplanev1alpha1.PublicationDomain, error) {
	response, err := c.api.PutPublicationDomain(ctx, id, &controlplanev1alpha1.PutPublicationDomainParams{IfMatch: expected}, input)
	if err != nil {
		return controlplanev1alpha1.PublicationDomain{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return controlplanev1alpha1.PublicationDomain{}, responseError(response)
	}
	var result controlplanev1alpha1.PublicationDomain
	err = decode(response, &result)
	return result, err
}

func (c *Client) PutPublicationGrant(ctx context.Context, domainID, workspaceID, bindingID string) error {
	response, err := c.api.PutPublicationGrant(ctx, domainID, workspaceID, bindingID)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return responseError(response)
	}
	return nil
}

func (c *Client) DeletePublicationGrant(ctx context.Context, domainID, workspaceID, bindingID string) error {
	response, err := c.api.DeletePublicationGrant(ctx, domainID, workspaceID, bindingID)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return responseError(response)
	}
	return nil
}

func (c *Client) DeletePublicationDomain(ctx context.Context, domainID string, version int) error {
	response, err := c.api.DeletePublicationDomain(ctx, domainID, &controlplanev1alpha1.DeletePublicationDomainParams{IfMatch: version})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return responseError(response)
	}
	return nil
}

func (c *Client) DeletePublicationBinding(ctx context.Context, clusterID string, revision int) error {
	response, err := c.api.DeleteClusterPublicationBinding(ctx, clusterID, &controlplanev1alpha1.DeleteClusterPublicationBindingParams{IfMatch: revision})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return responseError(response)
	}
	return nil
}

func (c *Client) PublicationDependents(ctx context.Context, domainID, bindingID, cursor string, limit int) ([]controlplanev1alpha1.PublicationDependent, string, error) {
	params := &controlplanev1alpha1.GetPublicationDependentsParams{DomainId: &domainID, BindingId: &bindingID, Limit: &limit}
	if cursor != "" {
		params.Cursor = &cursor
	}
	response, err := c.api.GetPublicationDependents(ctx, params)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", responseError(response)
	}
	var page struct {
		Items      []controlplanev1alpha1.PublicationDependent `json:"items"`
		NextCursor *string                                     `json:"nextCursor"`
	}
	if err = decode(response, &page); err != nil {
		return nil, "", err
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	return page.Items, next, nil
}
