package telemt

import (
	"context"
	"net/http"

	"tgwebproxy/internal/reliability"
)

type DCStats struct {
	MiddleProxyEnabled bool             `json:"middle_proxy_enabled"`
	DCs                []reliability.DC `json:"dcs"`
}

func (c *Client) DCStats(ctx context.Context) (DCStats, error) {
	var d DCStats
	_, err := c.do(ctx, http.MethodGet, "/v1/stats/dcs", nil, &d)
	return d, err
}
