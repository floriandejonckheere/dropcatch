// Command fakeregistry is an RDAP server and a stand-in for OVH's VPS API,
// for development and the screenshots, so that they need no network
// access. Point Dropcatch at it with
// --rdap-bootstrap http://127.0.0.1:8053/dns.json --ovh-api http://127.0.0.1:8053/v1.
//
// RDAP: it answers for every TLD. Names starting with "free" are
// available, names starting with "drop" are pending deletion, and all
// others are registered, expiring a number of days from now that depends
// on the name.
//
// OVH: it offers four VPS plans for every subsidiary. VPS-1 is out of
// stock everywhere (until --restock-after, then in Roubaix for
// --restock-for, again and again), and the others
// have stock in some datacenters.
//
// Hetzner Cloud: it lists the current server types (shared x86 and Arm,
// dedicated x86) in the six locations, for any API token. CAX11 is out of
// stock everywhere (and back in Falkenstein like VPS-1 with
// --restock-after), and the others are unavailable in some locations.
package main

import (
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8053", "address to listen on")
	restock := flag.Duration("restock-after", 0, "put VPS-1 in stock in Roubaix this long after starting (0 = never)")
	restockFor := flag.Duration("restock-for", 0, "sell VPS-1 out again after this long in stock, and restock it after --restock-after (0 = never sell out)")
	tlds := flag.String("tlds", "com,net,org,io,dev,app,shop,be,nl,de,co.uk", "comma-separated TLDs to serve")
	flag.Parse()
	started := time.Now()

	http.HandleFunc("GET /dns.json", func(w http.ResponseWriter, r *http.Request) {
		var list []string
		for _, t := range strings.Split(*tlds, ",") {
			list = append(list, fmt.Sprintf("%q", strings.TrimSpace(t)))
		}
		fmt.Fprintf(w, `{"version":"1.0","services":[[[%s],["http://%s/rdap/"]]]}`, strings.Join(list, ","), r.Host)
	})
	http.HandleFunc("GET /rdap/domain/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := strings.ToLower(r.PathValue("name"))
		label := name[:strings.Index(name+".", ".")]
		h := fnv.New32a()
		h.Write([]byte(name))
		days := int(h.Sum32()%700) + 10
		status := `"client transfer prohibited"`
		switch {
		case strings.HasPrefix(label, "free"):
			http.NotFound(w, r)
			return
		case strings.HasPrefix(label, "drop"):
			status, days = `"pending delete","redemption period"`, -35
		}
		created := time.Now().AddDate(-int(h.Sum32()%15)-1, 0, 0)
		w.Header().Set("Content-Type", "application/rdap+json")
		fmt.Fprintf(w, `{"objectClassName":"domain","ldhName":%q,"status":[%s],
			"events":[{"eventAction":"registration","eventDate":%q},{"eventAction":"expiration","eventDate":%q}],
			"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Example Registrar, Inc."]]]}]}`,
			name, status, created.Format(time.RFC3339), time.Now().AddDate(0, 0, days).Format(time.RFC3339))
	})
	http.HandleFunc("GET /v1/order/catalog/public/vps", func(w http.ResponseWriter, r *http.Request) {
		var plans, products []string
		for i, p := range vpsPlans {
			plans = append(plans, fmt.Sprintf(`{"planCode":%[1]q,"invoiceName":%[2]q,"product":%[1]q,"blobs":{"tags":["order-funnel:show"]},
				"pricings":[{"capacities":["renew"],"interval":1,"intervalUnit":"month","price":%[3]d}]}`, p.code, p.name, p.price))
			products = append(products, fmt.Sprintf(`{"name":%q,"blobs":{"technical":{"cpu":{"cores":%d},"memory":{"size":%d},
				"storage":{"disks":[{"capacity":%d,"technology":"NVMe"}]}}}}`, p.code, 2+2*i, 4<<i, 75*(i+1)))
		}
		fmt.Fprintf(w, `{"locale":{"currencyCode":"EUR"},"plans":[%s],"products":[%s]}`, strings.Join(plans, ","), strings.Join(products, ","))
	})
	http.HandleFunc("GET /v1/vps/order/rule/datacenter", func(w http.ResponseWriter, r *http.Request) {
		plan := r.URL.Query().Get("planCode")
		var stock map[string]bool
		for _, p := range vpsPlans {
			if p.code == plan {
				stock = p.stock
			}
		}
		var dcs []string
		for _, code := range []string{"eu-west-gra", "eu-west-rbx", "eu-west-sbg", "eu-west-lim", "eu-west-eri", "eu-central-waw", "eu-south-mil", "ca-east-bhs", "ap-southeast-sgp", "ap-southeast-syd"} {
			if stock == nil {
				break
			}
			status := "out-of-stock"
			if stock[code] || (plan == vpsPlans[0].code && code == "eu-west-rbx" && restocked(time.Since(started), *restock, *restockFor)) {
				status = "available"
			}
			dcs = append(dcs, fmt.Sprintf(`{"datacenter":%q,"code":%q,"status":%q,"linuxStatus":%[3]q}`,
				strings.ToUpper(code[strings.LastIndex(code, "-")+1:]), code, status))
		}
		fmt.Fprintf(w, `{"datacenters":[%s]}`, strings.Join(dcs, ","))
	})
	http.HandleFunc("GET /v1/locations", func(w http.ResponseWriter, r *http.Request) {
		if !hetznerAuth(w, r) {
			return
		}
		var locs []string
		for i, l := range hetznerLocations {
			locs = append(locs, fmt.Sprintf(`{"id":%d,"name":%q,"description":%q,"country":%q,"city":%q,"network_zone":%q}`, i+1, l.code, l.city, l.country, l.city, l.zone))
		}
		fmt.Fprintf(w, `{"locations":[%s],"meta":{"pagination":{"page":1,"per_page":50,"next_page":null,"last_page":1,"total_entries":%d}}}`, strings.Join(locs, ","), len(locs))
	})
	http.HandleFunc("GET /v1/server_types", func(w http.ResponseWriter, r *http.Request) {
		if !hetznerAuth(w, r) {
			return
		}
		name := r.URL.Query().Get("name")
		var types []string
		for i, t := range hetznerTypes {
			if name != "" && t.name != name {
				continue
			}
			var prices, locs []string
			for j, l := range hetznerLocations {
				if t.arch == "arm" && !strings.HasPrefix(l.zone, "eu") {
					continue // Arm servers are only in Europe
				}
				price := fmt.Sprintf("%.4f", t.price*(1+0.2*float64(j/3)))
				prices = append(prices, fmt.Sprintf(`{"location":%q,"price_hourly":{"net":"0.0100","gross":"0.0119"},"price_monthly":{"net":%q,"gross":%q},"included_traffic":21990232555520,"price_per_tb_traffic":{"net":"1.0000","gross":"1.1900"}}`, l.code, price, price))
				// A fixed pattern of locations without stock.
				avail := (i+j)%4 != 0
				if t.name == "cax11" {
					avail = l.code == "fsn1" && restocked(time.Since(started), *restock, *restockFor)
				}
				locs = append(locs, fmt.Sprintf(`{"id":%d,"name":%q,"deprecation":null,"available":%t,"recommended":false}`, j+1, l.code, avail))
			}
			types = append(types, fmt.Sprintf(`{"id":%d,"name":%q,"description":%q,"cores":%d,"memory":%d.0,"disk":%d,"deprecated":false,"deprecation":null,
				"prices":[%s],"storage_type":"local","cpu_type":%q,"architecture":%q,"category":%q,"locations":[%s]}`,
				100+i, t.name, strings.ToUpper(t.name), t.cores, t.memory, t.disk, strings.Join(prices, ","), t.cpu, t.arch, t.category, strings.Join(locs, ",")))
		}
		fmt.Fprintf(w, `{"server_types":[%s],"meta":{"pagination":{"page":1,"per_page":50,"previous_page":null,"next_page":null,"last_page":1,"total_entries":%d}}}`, strings.Join(types, ","), len(types))
	})
	log.Printf("fake registry on http://%s/dns.json", *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

// hetznerAuth requires a token, as the Hetzner Cloud API does.
func hetznerAuth(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"unable to authenticate"}}`)
		return false
	}
	return true
}

var hetznerLocations = []struct{ code, city, country, zone string }{
	{"fsn1", "Falkenstein", "DE", "eu-central"},
	{"nbg1", "Nuremberg", "DE", "eu-central"},
	{"hel1", "Helsinki", "FI", "eu-central"},
	{"ash", "Ashburn, VA", "US", "us-east"},
	{"hil", "Hillsboro, OR", "US", "us-west"},
	{"sin", "Singapore", "SG", "ap-southeast"},
}

var hetznerTypes = []struct {
	name, cpu, arch, category string
	cores, memory, disk       int
	price                     float64
}{
	{"cx23", "shared", "x86", "cost_optimized", 2, 4, 40, 3.49},
	{"cx33", "shared", "x86", "cost_optimized", 4, 8, 80, 5.49},
	{"cx43", "shared", "x86", "cost_optimized", 8, 16, 160, 9.49},
	{"cx53", "shared", "x86", "cost_optimized", 16, 32, 320, 17.49},
	{"cpx22", "shared", "x86", "regular_purpose", 2, 4, 80, 6.49},
	{"cpx32", "shared", "x86", "regular_purpose", 4, 8, 160, 10.99},
	{"cpx42", "shared", "x86", "regular_purpose", 8, 16, 320, 19.99},
	{"cax11", "shared", "arm", "cost_optimized", 2, 4, 40, 3.79},
	{"cax21", "shared", "arm", "cost_optimized", 4, 8, 80, 6.49},
	{"cax31", "shared", "arm", "cost_optimized", 8, 16, 160, 12.49},
	{"cax41", "shared", "arm", "cost_optimized", 16, 32, 320, 24.49},
	{"ccx13", "dedicated", "x86", "general_purpose", 2, 8, 80, 12.49},
	{"ccx23", "dedicated", "x86", "general_purpose", 4, 16, 160, 24.49},
	{"ccx33", "dedicated", "x86", "general_purpose", 8, 32, 240, 48.49},
}

// restocked reports whether VPS-1 is in stock after running for d: from
// after on, and with restockFor set, alternately out of stock for after and
// in stock for restockFor.
func restocked(d, after, restockFor time.Duration) bool {
	switch {
	case after <= 0 || d < after:
		return false
	case restockFor <= 0:
		return true
	}
	return (d-after)%(after+restockFor) < restockFor
}

var vpsPlans = []struct {
	code, name string
	price      int64 // in hundred-millionths, as in OVH's catalog
	stock      map[string]bool
}{
	{"vps-2025-model1", "VPS-1", 420000000, map[string]bool{}},
	{"vps-2025-model2", "VPS-2", 770000000, map[string]bool{"eu-west-gra": true, "ca-east-bhs": true}},
	{"vps-2025-model3", "VPS-3", 1540000000, map[string]bool{"eu-west-sbg": true, "eu-west-lim": true, "ap-southeast-sgp": true}},
	{"vps-2025-model4", "VPS-4", 2580000000, map[string]bool{"eu-central-waw": true}},
}
