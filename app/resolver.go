// Copyright 2021 Raphaël P. Barazzutti
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"github.com/domainr/dnsr"
	"github.com/miekg/dns"
	"net"
	"strings"
)

type resolver struct {
	config *Config
	cache  map[string]*net.IPAddr
}

func newResolver(config *Config) *resolver {
	return &resolver{
		config: config,
		cache:  make(map[string]*net.IPAddr),
	}
}

func (resolver *resolver) resolveConn(addr string) (string, error) {
	if host, port, err := net.SplitHostPort(addr); err != nil {
		return "", err
	} else if resolved, err := resolver.resolve(host); err != nil {
		return "", err
	} else {
		if strings.Contains(resolved.IP.String(), ":") {
			return fmt.Sprintf("[%s]:%s", resolved, port), nil
		}
		return fmt.Sprintf("%s:%s", resolved, port), nil
	}
}

func resolveWithSpecificServerQtype(qtype uint16, server string, host string) ([]*net.IP, error) {
	var ips []*net.IP

	msg := new(dns.Msg)
	msg.Id = dns.Id()
	msg.RecursionDesired = true
	msg.Question = []dns.Question{}

	msg.Question = append(msg.Question, dns.Question{Name: host, Qtype: qtype, Qclass: dns.ClassINET})

	c := new(dns.Client)

	in, _, err := c.Exchange(msg, fmt.Sprintf("%s:53", server))

	if err != nil {
		return nil, err
	}

	for _, a := range in.Answer {
		if ipv4, ok := a.(*dns.A); ok {
			ips = append(ips, &ipv4.A)
		} else if ipv6, ok := a.(*dns.AAAA); ok {
			ips = append(ips, &ipv6.AAAA)
		}
	}
	return ips, nil
}

func resolveWithSpecificServer(network, server string, host string) ([]*net.IP, error) {

	type resolveAnswer struct {
		ip    []*net.IP
		err   error
		qtype uint16
	}
	if network == "ip4" {
		return resolveWithSpecificServerQtype(dns.TypeA, server, host)
	} else if network == "ip6" {
		return resolveWithSpecificServerQtype(dns.TypeAAAA, server, host)
	} else {
		var ips []*net.IP

		answersChan := make(chan *resolveAnswer)
		ret := func(qtype uint16) {
			out, err := resolveWithSpecificServerQtype(qtype, server, host)

			answersChan <- &resolveAnswer{out, err, qtype}

		}
		go ret(dns.TypeA)
		go ret(dns.TypeAAAA)

		oneSucceeded := false
		for i := 0; i < 2; i++ {
			if answer := <-answersChan; answer.err == nil {

				if len(answer.ip) > 0 {
					if answer.qtype == dns.TypeA {
						return answer.ip, nil
					}
					ips = append(ips, answer.ip...)
				}

				oneSucceeded = true
			}

		}

		if !oneSucceeded {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return ips, nil
	}

}

func (resolver *resolver) resolve(addr string) (*net.IPAddr, error) {
	if val, ok := resolver.cache[addr]; ok {
		return val, nil
	}

	resolvedAddr, err := resolver.actualResolve(addr)
	if err != nil {
		return nil, err
	}

	if resolver.config.CacheDNSRequests {
		resolver.cache[addr] = resolvedAddr
	}
	return resolvedAddr, err
}

func (resolver *resolver) actualResolve(addr string) (*net.IPAddr, error) {

	if resolver.config.FullDNS {
		var ip net.IP

		if ip = net.ParseIP(addr); ip == nil {
			if entries, err := resolver.fullResolveFromRoot(resolver.config.IPProtocol, addr); err == nil {
				ip = net.ParseIP(*entries)
			}
		}
		if ip == nil {
			return nil, &net.DNSError{Err: "no such host", Name: addr, IsNotFound: true}
		}
		return &net.IPAddr{IP: ip}, nil
	} else if resolver.config.DNSServer != "" {
		ip, err := resolveWithSpecificServer(resolver.config.IPProtocol, resolver.config.DNSServer, fmt.Sprintf("%s.", addr))
		if err != nil {
			return nil, err
		}

		if len(ip) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: addr, IsNotFound: true}
		}

		return &net.IPAddr{IP: *ip[0]}, nil
	} else {
		return net.ResolveIPAddr(resolver.config.IPProtocol, addr)
	}
}

func (*resolver) fullResolveFromRoot(network, host string) (*string, error) {
	var qtypes []string

	if network == "ip" {
		qtypes = []string{"A", "AAAA"}
	} else if network == "ip4" {
		qtypes = []string{"A"}
	} else if network == "ip6" {
		qtypes = []string{"AAAA"}
	} else {
		qtypes = []string{}
	}

	r := dnsr.New(1024)
	requestCount := 0

	var resolveRecu func(r *dnsr.Resolver, host string) (*string, error)

	resolveRecu = func(r *dnsr.Resolver, host string) (*string, error) {
		requestCount++
		cnames := make(map[string]struct{})
		for _, qtype := range qtypes {
			for _, rr := range r.Resolve(host, qtype) {
				if rr.Type == qtype {
					return &rr.Value, nil
				} else if rr.Type == "CNAME" {
					cnames[rr.Value] = struct{}{}
				}
			}
		}

		for cname := range cnames {
			return resolveRecu(r, cname)
		}

		return nil, fmt.Errorf("no host found: %s", host)
	}

	return resolveRecu(r, host)
}
