package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	group := flag.String("group", "iot", "iot or android release targets")
	flag.Parse()
	if err := printMatrix(*group); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printMatrix(group string) error {
	if group != "iot" && group != "android" {
		return fmt.Errorf("unknown release group: %s", group)
	}
	data, err := os.ReadFile(".github/workflows/build.yml")
	if err != nil {
		return err
	}
	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]any
				}
			}
		}
	}
	if err = yaml.Unmarshal(data, &workflow); err != nil {
		return err
	}
	var targets []map[string]any
	for _, target := range workflow.Jobs["build"].Strategy.Matrix.Include {
		if group == "iot" && target["libc"] == "musl" || group == "android" && target["os"] == "android" {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("no %s targets in release workflow", group)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"include": targets})
}
