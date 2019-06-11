// Copyright (C) 2019 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/algorand/go-algorand/tools/network"
	"github.com/algorand/go-algorand/tools/network/cloudflare"
)

var (
	addFromName    string
	addToAddress   string
	deleteNetwork  string
	listNetwork    string
	recordType     string
	noPrompt       bool
	excludePattern string
)

func init() {
	dnsCmd.AddCommand(checkCmd)
	dnsCmd.AddCommand(addCmd)
	dnsCmd.AddCommand(deleteCmd)
	dnsCmd.AddCommand(listCmd)

	addCmd.Flags().StringVarP(&addFromName, "from", "f", "", "From name to add new DNS entry")
	addCmd.MarkFlagRequired("from")
	addCmd.Flags().StringVarP(&addToAddress, "to", "t", "", "To address to map new DNS entry to")
	addCmd.MarkFlagRequired("to")

	deleteCmd.Flags().StringVarP(&deleteNetwork, "network", "n", "", "Network name for records to delete")
	deleteCmd.MarkFlagRequired("network")
	deleteCmd.Flags().BoolVarP(&noPrompt, "no-prompt", "y", false, "No prompting for records deletion")
	deleteCmd.Flags().StringVarP(&excludePattern, "exclude", "e", "", "name records exclude pattern")

	listCmd.Flags().StringVarP(&listNetwork, "network", "n", "", "Domain name for records to list")
	listCmd.Flags().StringVarP(&recordType, "recordType", "t", "", "DNS record type to list (A, CNAME, SRV)")
	listCmd.MarkFlagRequired("network")
}

type byIP []net.IP

func (a byIP) Len() int           { return len(a) }
func (a byIP) Less(i, j int) bool { return a[i].String() < a[j].String() }
func (a byIP) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "Check status of required DNS entries",
	Long:  "Check status of required DNS entries",
	Run: func(cmd *cobra.Command, args []string) {
		// Fall back
		cmd.HelpFunc()(cmd, args)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the DNS/SRV entries of the given network",
	Long:  "List the DNS/SRV entries of the given network",
	Run: func(cmd *cobra.Command, args []string) {
		recordType = strings.ToUpper(recordType)
		if recordType == "" || recordType == "A" || recordType == "CNAME" || recordType == "SRV" {
			listEntries(listNetwork, recordType)
		} else {
			fmt.Fprintf(os.Stderr, "Invalid recordType specified.\n")
			os.Exit(1)
		}
	},
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check the status",
	Long:  "Check the status",
	Run: func(cmd *cobra.Command, args []string) {
		checkDNSRecord("relay-us-ea-1.algorand.network")
		checkDNSRecord("relay-us-ea-2.algorand.network")
		checkDNSRecord("relay-us-ea-3.algorand.network")
		checkDNSRecord("relay-us-ea-4.algorand.network")
		checkDNSRecord("relay-us-ea-99876.algorand.network")

		fmt.Printf("------------------------\n")
		checkSrvRecord("devnet.algorand.network")
		checkSrvRecord("testnet.algorand.network")
		checkSrvRecord("bogus.algorand.network")
		fmt.Printf("------------------------\n")
	},
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a DNS record",
	Long:  "Adds a DNS record to map --from to --to, using A if to == IP or CNAME otherwise\n",
	Example: "algons dns add -f a.test.algodev.network -t r1.algodev.network\n" +
		"algons dns add -f a.test.algodev.network -t 192.168.100.10",
	Run: func(cmd *cobra.Command, args []string) {
		err := doAddDNS(addFromName, addToAddress)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding DNS entry: %v\n", err)
			os.Exit(1)
		} else {
			fmt.Printf("DNS Entry Added\n")
		}
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete DNS and SRV records for a specified network",
	Run: func(cmd *cobra.Command, args []string) {
		if !doDeleteDNS(deleteNetwork, noPrompt, excludePattern) {
			os.Exit(1)
		}
	},
}

func doAddDNS(from string, to string) (err error) {
	cfZoneID, cfEmail, cfKey, err := getClouldflareCredentials()
	if err != nil {
		return fmt.Errorf("error getting DNS credentials: %v", err)
	}

	cloudflareDNS := cloudflare.NewDNS(cfZoneID, cfEmail, cfKey)

	const priority = 1
	const proxied = false

	// If we need to register anything, first register a DNS entry
	// to map our network DNS name to our public name (or IP) provided to nodecfg
	// Network HostName = eg r1.testnet.algorand.network
	isIP := net.ParseIP(to) != nil
	var recordType string
	if isIP {
		recordType = "A"
	} else {
		recordType = "CNAME"
	}
	cloudflareDNS.SetDNSRecord(context.Background(), recordType, from, to, cloudflare.AutomaticTTL, priority, proxied)

	return
}

func getClouldflareCredentials() (zoneID string, email string, authKey string, err error) {
	zoneID = os.Getenv("CLOUDFLARE_ZONE_ID")
	email = os.Getenv("CLOUDFLARE_EMAIL")
	authKey = os.Getenv("CLOUDFLARE_AUTH_KEY")
	if zoneID == "" || email == "" || authKey == "" {
		err = fmt.Errorf("one or more credentials missing from ENV")
	}
	return
}

func checkDNSRecord(dnsName string) {
	fmt.Printf("------------------------\nDNS Lookup: %s\n", dnsName)
	ips, err := net.LookupIP(dnsName)
	if err != nil {
		fmt.Printf("Cannot resolve %s: %v\n", dnsName, err)
	} else {
		sort.Sort(byIP(ips))
		for _, ip := range ips {
			fmt.Printf("-> %s\n", ip.String())
		}
	}
}

func checkSrvRecord(dnsBootstrap string) {
	fmt.Printf("------------------------\nSRV Lookup: %s\n", dnsBootstrap)

	_, addrs, err := net.LookupSRV("algobootstrap", "tcp", dnsBootstrap)
	if err != nil {
		if !strings.HasSuffix(err.Error(), "cannot unmarshal DNS message") {
			// we weren't able to get the SRV records.
			fmt.Printf("Cannot lookup SRV record for %s: %v\n", dnsBootstrap, err)
			return
		}

		var resolver network.Resolver
		_, addrs, err = resolver.LookupSRV(context.Background(), "algobootstrap", "tcp", dnsBootstrap)
		if err != nil {
			fmt.Printf("Cannot lookup SRV record for %s via neither default resolver nor via %s: %v\n", dnsBootstrap, resolver.EffectiveResolverDNS(), err)
			return
		}
	}

	for _, srv := range addrs {
		fmt.Printf("%s:%d\n", srv.Target, srv.Port)
	}
}

func doDeleteDNS(network string, noPrompt bool, excludePattern string) bool {

	if network == "" || network == "testnet" || network == "devnet" || network == "mainnet" {
		fmt.Fprintf(os.Stderr, "Deletion of network '%s' using this tool is not allowed\n", network)
		return false
	}

	var excludeRegex *regexp.Regexp
	if excludePattern != "" {
		var err error
		excludeRegex, err = regexp.Compile(excludePattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "specified regular expression exclude pattern ('%s') is not a valid regular expression : %v", excludePattern, err)
			return false
		}
	}

	cfZoneID, cfEmail, cfKey, err := getClouldflareCredentials()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting DNS credentials: %v", err)
		return false
	}

	cloudflareDNS := cloudflare.NewDNS(cfZoneID, cfEmail, cfKey)

	idsToDelete := make(map[string]string) // Maps record ID to Name

	for _, service := range []string{"_algobootstrap", "_metrics"} {
		records, err := cloudflareDNS.ListDNSRecord(context.Background(), "SRV", service+"._tcp."+network+".algodev.network", "", "", "", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing SRV '%s' entries: %v\n", service, err)
			os.Exit(1)
		}
		for _, r := range records {
			if excludeRegex != nil {
				if excludeRegex.MatchString(r.Name) {
					fmt.Printf("Excluding SRV '%s' record: %s\n", service, r.Name)
					continue
				}
			}
			fmt.Printf("Found SRV '%s' record: %s\n", service, r.Name)
			idsToDelete[r.ID] = r.Name
		}
	}

	networkSuffix := "." + network + ".algodev.network"

	for _, recordType := range []string{"A", "CNAME"} {
		records, err := cloudflareDNS.ListDNSRecord(context.Background(), recordType, "", "", "", "", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing DNS '%s' entries: %v\n", recordType, err)
			os.Exit(1)
		}
		for _, r := range records {
			if strings.Index(r.Name, networkSuffix) > 0 {
				if excludeRegex != nil {
					if excludeRegex.MatchString(r.Name) {
						fmt.Printf("Excluding DNS '%s' record: %s\n", recordType, r.Name)
						continue
					}
				}
				fmt.Printf("Found DNS '%s' record: %s\n", recordType, r.Name)
				idsToDelete[r.ID] = r.Name
			}
		}
	}

	if len(idsToDelete) == 0 {
		fmt.Printf("No DNS/SRV records found\n")
		return true
	}

	var text string
	if !noPrompt {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Delete these %d entries (type 'yes' to delete)? ", len(idsToDelete))
		text, _ = reader.ReadString('\n')
		text = strings.Replace(text, "\n", "", -1)
	} else {
		text = "yes"
	}

	if text == "yes" {
		for id, name := range idsToDelete {
			fmt.Fprintf(os.Stdout, "Deleting %s\n", name)
			err = cloudflareDNS.DeleteDNSRecord(context.Background(), id)
			if err != nil {
				fmt.Fprintf(os.Stderr, " !! error deleting %s: %v\n", name, err)
			}
		}
	}
	return true
}

func listEntries(listNetwork string, recordType string) {
	cfZoneID, cfEmail, cfKey, err := getClouldflareCredentials()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting DNS credentials: %v", err)
		return
	}

	cloudflareDNS := cloudflare.NewDNS(cfZoneID, cfEmail, cfKey)
	recordTypes := []string{"A", "CNAME", "SRV"}
	if recordType != "" {
		recordTypes = []string{recordType}
	}
	for _, recType := range recordTypes {
		records, err := cloudflareDNS.ListDNSRecord(context.Background(), recType, "", "", "", "", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing DNS entries: %v\n", err)
			os.Exit(1)
		}

		for _, record := range records {
			if strings.HasSuffix(record.Name, listNetwork) {
				fmt.Printf("%v\n", record.Name)
			}
		}
	}
}
