package ifunny

type IFunnyConfig struct {
	BearerToken string `psy:"bearer_token"`
	UserAgent   string `psy:"user_agent"`
}
