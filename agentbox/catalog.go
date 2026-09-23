package agentbox

import (
	"context"
	"net/http"
)

// Idc is a data center entry from GET /idcs.
type Idc struct {
	Data map[string]any
}

// IdcID is the data center identifier. Use this (not Name) when selecting
// an Agent or Sandbox location.
func (i *Idc) IdcID() string {
	if v := stringField(i.Data, "idcId"); v != "" {
		return v
	}
	return stringField(i.Data, "idc_id")
}

// Name is the data center's display name.
func (i *Idc) Name() string { return stringField(i.Data, "name") }

// IdcCollection provides operations on data centers (client.Idcs).
type IdcCollection struct {
	client *Client
}

// List returns the available data centers, optionally filtered by runtime.
func (ic *IdcCollection) List(ctx context.Context, runtime string) ([]*Idc, error) {
	var payload struct {
		Idcs []map[string]any `json:"idcs"`
	}
	err := ic.client.request(ctx, http.MethodGet, "/idcs", compact(map[string]any{
		"runtime": nonEmptyString(runtime),
	}), nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Idc, 0, len(payload.Idcs))
	for _, item := range payload.Idcs {
		items = append(items, &Idc{Data: item})
	}
	return items, nil
}

// Product is a SKU entry from GET /products.
type Product struct {
	Data map[string]any
}

// InstanceType is the SKU identifier.
func (p *Product) InstanceType() string {
	if v := stringField(p.Data, "instanceType"); v != "" {
		return v
	}
	return stringField(p.Data, "instance_type")
}

// Price is the price reported by the service.
func (p *Product) Price() float64 {
	switch v := p.Data["price"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// ProductCollection provides operations on SKUs (client.Products).
type ProductCollection struct {
	client *Client
}

// ProductListParams configures ProductCollection.List. IdcName is a data
// center identifier from IdcCollection.List, not a region label.
type ProductListParams struct {
	IdcName string
	Runtime string
}

// List returns the available SKUs, optionally filtered by data center and
// runtime.
func (pc *ProductCollection) List(ctx context.Context, params ProductListParams) ([]*Product, error) {
	var payload struct {
		Products []map[string]any `json:"products"`
	}
	err := pc.client.request(ctx, http.MethodGet, "/products", compact(map[string]any{
		"idc_name": nonEmptyString(params.IdcName),
		"runtime":  nonEmptyString(params.Runtime),
	}), nil, nil, &payload)
	if err != nil {
		return nil, err
	}
	items := make([]*Product, 0, len(payload.Products))
	for _, item := range payload.Products {
		items = append(items, &Product{Data: item})
	}
	return items, nil
}
