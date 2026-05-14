package ddnsprovider

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultAliDNSEndpoint = "https://alidns.aliyuncs.com/"

type AliDNS struct {
	accessKeyID     string
	accessKeySecret string
	endpoint        string
	httpClient      *http.Client
}

type aliDNSRecord struct {
	DomainName string `json:"DomainName"`
	RecordID   string `json:"RecordId"`
	Value      string `json:"Value"`
}

type aliDNSRecordsResponse struct {
	TotalCount    int `json:"TotalCount"`
	DomainRecords struct {
		Record []aliDNSRecord `json:"Record"`
	} `json:"DomainRecords"`
}

type aliDNSResponse struct {
	RecordID string `json:"RecordId"`
}

func NewAliDNS(cfg map[string]string) (Updater, error) {
	accessKeyID := strings.TrimSpace(cfg["access_key_id"])
	accessKeySecret := strings.TrimSpace(cfg["access_key_secret"])
	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("alidns access_key_id and access_key_secret are required")
	}
	endpoint := strings.TrimSpace(cfg["endpoint"])
	if endpoint == "" {
		endpoint = defaultAliDNSEndpoint
	}
	return &AliDNS{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		endpoint:        endpoint,
		httpClient:      http.DefaultClient,
	}, nil
}

func validateAliDNSConfig(cfg map[string]string) error {
	return validateProviderOptions("alidns", cfg, []string{"access_key_id", "access_key_secret"}, []string{"access_key_id", "access_key_secret"})
}

func (a *AliDNS) Name() string {
	return "alidns"
}

func (a *AliDNS) Upsert(ctx context.Context, record Record) error {
	resolved, err := ResolveRecordName(record.Zone, record.Name)
	if err != nil {
		return err
	}
	params := url.Values{}
	params.Set("Action", "DescribeSubDomainRecords")
	params.Set("DomainName", resolved.Zone)
	params.Set("SubDomain", resolved.AliDNSSubDomain())
	params.Set("Type", string(record.Type))

	var records aliDNSRecordsResponse
	if err := a.request(ctx, params, &records); err != nil {
		return err
	}

	if records.TotalCount > 0 {
		selected := records.DomainRecords.Record[0]
		if selected.Value == record.Value {
			return nil
		}
		return a.update(ctx, resolved, selected, record)
	}
	return a.create(ctx, resolved, record)
}

func (a *AliDNS) create(ctx context.Context, resolved RecordName, record Record) error {
	params := url.Values{}
	params.Set("Action", "AddDomainRecord")
	params.Set("DomainName", resolved.Zone)
	params.Set("RR", resolved.Relative)
	params.Set("Type", string(record.Type))
	params.Set("Value", record.Value)
	params.Set("TTL", strconv.Itoa(defaultAliDNSTTL(record.TTL)))
	var response aliDNSResponse
	if err := a.request(ctx, params, &response); err != nil {
		return err
	}
	if response.RecordID == "" {
		return fmt.Errorf("alidns create returned empty record id")
	}
	return nil
}

func (a *AliDNS) update(ctx context.Context, resolved RecordName, existing aliDNSRecord, record Record) error {
	params := url.Values{}
	params.Set("Action", "UpdateDomainRecord")
	params.Set("RecordId", existing.RecordID)
	params.Set("RR", resolved.Relative)
	params.Set("Type", string(record.Type))
	params.Set("Value", record.Value)
	params.Set("TTL", strconv.Itoa(defaultAliDNSTTL(record.TTL)))
	var response aliDNSResponse
	if err := a.request(ctx, params, &response); err != nil {
		return err
	}
	if response.RecordID == "" {
		return fmt.Errorf("alidns update returned empty record id")
	}
	return nil
}

func (a *AliDNS) request(ctx context.Context, params url.Values, out any) error {
	method := http.MethodGet
	aliyunSigner(a.accessKeyID, a.accessKeySecret, &params, method, "2015-01-09")
	req, err := http.NewRequestWithContext(ctx, method, a.endpoint, bytes.NewBuffer(nil))
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()
	resp, err := a.httpClient.Do(req)
	return decodeJSONResponse(resp, err, out)
}

func defaultAliDNSTTL(ttl int) int {
	if ttl <= 0 {
		return 600
	}
	return ttl
}
