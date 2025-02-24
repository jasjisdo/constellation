/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package azure

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// subset of azure imds API: https://docs.microsoft.com/en-us/azure/virtual-machines/windows/instance-metadata-service?tabs=linux
// this is not yet available through the azure sdk (see https://github.com/Azure/azure-rest-api-specs/issues/4408)

const (
	imdsURL        = "http://169.254.169.254/metadata/instance"
	imdsAPIVersion = "2021-02-01"
	maxCacheAge    = 12 * time.Hour
)

type imdsClient struct {
	client *http.Client

	cache     metadataResponse
	cacheTime time.Time
}

// ProviderID returns the provider ID of the instance the function is called from.
func (c *imdsClient) ProviderID(ctx context.Context) (string, error) {
	if c.timeForUpdate() || c.cache.Compute.ResourceID == "" {
		if err := c.update(ctx); err != nil {
			return "", err
		}
	}

	if c.cache.Compute.ResourceID == "" {
		return "", errors.New("unable to get provider id")
	}

	return c.cache.Compute.ResourceID, nil
}

// SubscriptionID returns the subscription ID of the instance the function
// is called from.
func (c *imdsClient) SubscriptionID(ctx context.Context) (string, error) {
	if c.timeForUpdate() || c.cache.Compute.SubscriptionID == "" {
		if err := c.update(ctx); err != nil {
			return "", err
		}
	}

	if c.cache.Compute.SubscriptionID == "" {
		return "", errors.New("unable to get subscription id")
	}

	return c.cache.Compute.SubscriptionID, nil
}

// ResourceGroup returns the resource group of the instance the function
// is called from.
func (c *imdsClient) ResourceGroup(ctx context.Context) (string, error) {
	if c.timeForUpdate() || c.cache.Compute.ResourceGroup == "" {
		if err := c.update(ctx); err != nil {
			return "", err
		}
	}

	if c.cache.Compute.ResourceGroup == "" {
		return "", errors.New("unable to get resource group")
	}

	return c.cache.Compute.ResourceGroup, nil
}

// UID returns the UID of the cluster, based on the tags on the instance
// the function is calles from, which are inherited from the scale set.
func (c *imdsClient) UID(ctx context.Context) (string, error) {
	if c.timeForUpdate() || len(c.cache.Compute.Tags) == 0 {
		if err := c.update(ctx); err != nil {
			return "", err
		}
	}

	if len(c.cache.Compute.Tags) == 0 {
		return "", errors.New("unable to get uid")
	}

	for _, tag := range c.cache.Compute.Tags {
		if tag.Name == "uid" {
			return tag.Value, nil
		}
	}

	return "", errors.New("unable to get uid from metadata tags")
}

// timeForUpdate checks whether an update is needed due to cache age.
func (c *imdsClient) timeForUpdate() bool {
	return time.Since(c.cacheTime) > maxCacheAge
}

// update updates instance metadata from the azure imds API.
func (c *imdsClient) update(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imdsURL, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Add("Metadata", "True")
	query := req.URL.Query()
	query.Add("format", "json")
	query.Add("api-version", imdsAPIVersion)
	req.URL.RawQuery = query.Encode()
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var res metadataResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}

	c.cache = res
	c.cacheTime = time.Now()
	return nil
}

// metadataResponse contains metadataResponse with only the required values.
type metadataResponse struct {
	Compute metadataResponseCompute `json:"compute,omitempty"`
}

type metadataResponseCompute struct {
	ResourceID     string        `json:"resourceId,omitempty"`
	SubscriptionID string        `json:"subscriptionId,omitempty"`
	ResourceGroup  string        `json:"resourceGroupName,omitempty"`
	Tags           []metadataTag `json:"tagsList,omitempty"`
}

type metadataTag struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}
