package config

type Config struct {
	ListeningAddress string            `"json:listeningAddress"`
	ServiceList      map[string]string `"json:serviceList"`
}

var dConfig *Config

func GetDefaultConfig() *Config {
	if dConfig == nil {
		dConfig = &Config{
			ListeningAddress: ":7777",
			ServiceList:      map[string]string{},
		}
	}

	return dConfig
}
