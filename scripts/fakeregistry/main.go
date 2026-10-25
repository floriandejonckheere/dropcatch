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
	log.Printf("fake registry on http://%s/dns.json", *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
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
