package main

type Response struct {
	Description string `json:"description"`
}

type Operation struct {
	State                    string
	Description              string
	AsyncPollIntervalSeconds int `json:"async_poll_interval_seconds, omitempty"`
}

type CreateResponse struct {
	DashboardUrl  string
	LastOperation Operation
}

type serviceReq struct {
	ServiceId        string `json:"service_id"`
	PlanId           string `json:"plan_id"`
	OrganizationGuid string `json:"organization_guid"`
	SpaceGuid        string `json:"space_guid"`
}

type bindReq struct {
	ServiceId string `json:"service_id"`
	PlanId    string `json:"plan_id"`
	AppGuid   string `json:"app_guid"`
}
