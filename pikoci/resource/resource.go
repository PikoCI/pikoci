package resource

import "time"

type Resource struct {
	ID   uint32 `json:"id"`
	Type string `json:"type" hcl:"type,label"`
	Name string `json:"name" hcl:"name,label"`

	Params        *Params `json:"params,omitempty" hcl:"params,block"`
	CheckInterval string  `json:"check_interval" hcl:"check_interval,optional"`

	Canonical    string    `json:"canonical"`
	Logs         string    `json:"logs"`
	LastCheck    time.Time `json:"last_check"`
	NextCheck    time.Time `json:"next_check"`
	WebhookToken string    `json:"webhook_token"`
}

func (r Resource) GetParams() map[string]string {
	if r.Params == nil {
		return nil
	}
	return r.Params.Params
}

type Params struct {
	Params map[string]string `json:"params" hcl:",remain"`
}

type Version struct {
	ID      uint32                 `json:"id"`
	Version map[string]interface{} `json:"version"`
}
