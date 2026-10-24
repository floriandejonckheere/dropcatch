// Command fakeregistry is an RDAP server for the demo library and the
// screenshots, so that they need no network access. Point Dropcatch at it
// with --rdap-bootstrap http://127.0.0.1:8053/dns.json.
//
// It answers for every TLD. Names starting with "free" are available,
// names starting with "drop" are pending deletion, and all others are
// registered, expiring a number of days from now that depends on the name.
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
	tlds := flag.String("tlds", "com,net,org,io,dev,app,shop,be,nl,de,co.uk", "comma-separated TLDs to serve")
	flag.Parse()

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
	log.Printf("fake registry on http://%s/dns.json", *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
