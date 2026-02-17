package main

import "fmt"

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
