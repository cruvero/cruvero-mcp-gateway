package main

import "fmt"

func serverCommand(args []string) error {
	_ = args
	return fmt.Errorf("server command is not implemented")
}

func apikeyCommand(args []string) error {
	_ = args
	return fmt.Errorf("apikey command is not implemented")
}

func policyCommand(args []string) error {
	_ = args
	return fmt.Errorf("policy command is not implemented")
}

func healthCommand(args []string) error {
	_ = args
	return fmt.Errorf("health command is not implemented")
}

func migrateCommand(args []string) error {
	_ = args
	return fmt.Errorf("migrate command is not implemented")
}
