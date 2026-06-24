package webpush

// maxSubsPerUser caps how many endpoints a single user may register.
// Beyond this, AddSubscription silently drops further endpoints to keep
// client retries idempotent.
const maxSubsPerUser = 16

// Subscription is one browser's push endpoint. JSON shape is the same one
// the Browser Push API hands to the page (endpoint + keys).
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	CreatedAt int64 `json:"created_at"`
}
