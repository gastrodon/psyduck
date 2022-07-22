package ifunny

type IFunnyConfig struct {
	BearerAuth string `hcl:"bearer_auth"`
	UserAgent  string `hcl:"user_agent"`
}
