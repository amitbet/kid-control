package config

type Config struct {
	ListeningAddress string
	ServiceList      map[string]string
}

func GetDefaultConfig() *Config {

	dConfig := &Config{
		ListeningAddress: ":7777",
		ServiceList: map[string]string{
			"home":      "http://192.168.1.101:7777/",
			"kids":      "http://192.168.1.104:7777/",
			"bedroom":   "http://192.168.1.179:7777/",
			"localhost": "http://127.0.0.1:7777/",
		},
	}

	return dConfig
}
