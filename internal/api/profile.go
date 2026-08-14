package api

// Meta is the wire type for a profile (API payload).
type Meta struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	Enabled          *bool             `json:"enabled,omitempty"`
	URL              string            `json:"url,omitempty"`
	UserAgent        string            `json:"userAgent,omitempty"`
	UpdateInterval   *int              `json:"updateInterval,omitempty"`
	BaseProfileID    string            `json:"baseProfileId,omitempty"`
	ManagedBy        string            `json:"managedBy,omitempty"`
	EditorStatus     string            `json:"editorStatus,omitempty"`
	UpdatedAt        int64             `json:"updatedAt"`
	SubscriptionInfo *SubscriptionInfo `json:"subscriptionInfo,omitempty"`
	Active           bool              `json:"active,omitempty"` // derived: stamped by GET /profiles, never persisted
}

// SubscriptionInfo is the parsed Subscription-Userinfo response header
// (upload/download/total/expire, all in bytes / unix timestamp).
type SubscriptionInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}
