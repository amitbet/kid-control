package config

type Config struct {
	ServiceAddress string
	MachineList    map[string]string
}

func GetDefaultConfig() *Config {

	dConfig := &Config{
		ServiceAddress: ":7777",
		MachineList:    map[string]string{"home": "192.168.1.101", "kids": "192.168.1.104", "bedroom": "192.168.1.179"},
	}

	return dConfig
}
