package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/premday/sonic-tools/internal/view"
	"github.com/premday/sonic-tools/sonic"

	redis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
)

const timeout = 60 * time.Second

type descriptionResult struct {
	Interface      string
	OldDescription string
	NewDescription string
	Status         string
}

func renderResults(results []descriptionResult, dryRun bool) string {
	t := view.NewTable("Interface", "Old description", "New description", "Status")
	changed := 0
	for _, result := range results {
		newDescr := result.NewDescription
		if result.Status == "unchanged" {
			newDescr = "unchanged"
		}
		if result.Status == "updated" || result.Status == "dry-run" {
			changed++
		}
		t.Row(result.Interface, result.OldDescription, newDescr, result.Status)
	}

	summary := fmt.Sprintf("%d / %d interfaces changed", changed, len(results))
	if dryRun {
		summary += " (dry-run)"
	}

	return t.String() + "\n" + view.Comment(summary)
}

// descrRegex matches any character which is not alphanumeric, colon, hyphen, or dot.
var descrRegex = regexp.MustCompile(`[^a-zA-Z0-9:\-\.]+`)

func sanitizeDescription(description string) string {
	return descrRegex.ReplaceAllString(description, "")
}

func setDescription(ctx context.Context, rdb *redis.Client, intf, description string, dryRun bool) descriptionResult {
	result := descriptionResult{Interface: intf, NewDescription: sanitizeDescription(description)}

	port, err := sonic.FindPortConfig(ctx, rdb, intf)
	if err != nil {
		result.Status = fmt.Sprintf("error: %s", err)
		return result
	}
	result.OldDescription = port.Description

	switch {
	case result.NewDescription == result.OldDescription:
		result.Status = "unchanged"
	case dryRun:
		result.Status = "dry-run"
	default:
		if err := sonic.SetPortDescription(ctx, rdb, intf, result.NewDescription); err != nil {
			result.Status = fmt.Sprintf("error: %s", err)
			return result
		}
		result.Status = "updated"
	}

	return result
}

// setDescriptionFromLLDP names an interface after its LLDP neighbor: '<prefix><remote host>:<remote port>'.
func setDescriptionFromLLDP(ctx context.Context, rdb *redis.Client, lldp sonic.LLDP, intf, prefix string, dryRun bool) descriptionResult {
	neighbor := lldp.Neighbor(intf)
	if neighbor.Host == "" {
		port, err := sonic.FindPortConfig(ctx, rdb, intf)
		if err != nil {
			return descriptionResult{Interface: intf, Status: fmt.Sprintf("error: %s", err)}
		}
		return descriptionResult{
			Interface:      intf,
			OldDescription: port.Description,
			NewDescription: port.Description,
			Status:         "skipped",
		}
	}

	description := prefix + neighbor.Host
	if port := neighbor.PortName(); port != "" && port != "N/A" {
		description = fmt.Sprintf("%s:%s", description, port)
	}

	return setDescription(ctx, rdb, intf, description, dryRun)
}

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rdb := sonic.NewRedis()
	defer rdb.Close()

	dryRun := false
	verbose := false

	autoDescription := &cobra.Command{
		Use:   "auto-description <intf|all> [prefix]",
		Short: "Set interface description using LLDP data",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			lldp, err := sonic.LLDPNeighbors()
			if err != nil {
				return err
			}

			prefix := ""
			if len(args) > 1 {
				prefix = args[1]
			}

			results := []descriptionResult{}
			if args[0] != "all" {
				results = append(results, setDescriptionFromLLDP(ctx, rdb, lldp, args[0], prefix, dryRun))
			} else {
				neighbors, err := sonic.InterfaceNeighbors(ctx, rdb)
				if err != nil {
					return err
				}

				for intf, descr := range neighbors {
					if !strings.HasPrefix(descr, prefix) {
						continue
					}

					result := setDescriptionFromLLDP(ctx, rdb, lldp, intf, prefix, dryRun)
					if result.Status == "skipped" && !verbose {
						continue
					}
					results = append(results, result)
				}
			}

			fmt.Print("\n" + renderResults(results, dryRun) + "\n")

			return nil
		},
	}

	descrCmd := &cobra.Command{
		Use:   "description <intf> <description>",
		Short: "Set interface description",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			result := setDescription(ctx, rdb, args[0], args[1], dryRun)
			fmt.Print("\n" + renderResults([]descriptionResult{result}, dryRun) + "\n")

			return nil
		},
	}

	autoDescription.Flags().BoolVar(&dryRun, "dry-run", false, "do not apply changes")
	autoDescription.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose")
	descrCmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not apply changes")

	intfCmd := &cobra.Command{Use: "interface", Short: "Edit interfaces"}
	intfCmd.AddCommand(autoDescription, descrCmd)

	rootCmd := &cobra.Command{
		Use:           "premconfig",
		Short:         "Custom SONiC config CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(intfCmd)

	return rootCmd.Execute()
}
