package lookup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"
)

// whoisServer returns the WHOIS server (host:port) IANA lists for tld, or
// "" when there is none.
func (c *Client) whoisServer(ctx context.Context, tld string) (string, error) {
	c.mu.Lock()
	s, ok := c.whois[tld]
	c.mu.Unlock()
	if ok && time.Since(s.fetched) < bootstrapTTL {
		return s.addr, nil
	}
	text, err := c.whoisQuery(ctx, c.IANAWhois, tld)
	if err != nil {
		if ok {
			return s.addr, nil
		}
		return "", fmt.Errorf("asking IANA for the WHOIS server of .%s: %w", tld, err)
	}
	var addr string
	if m := regexp.MustCompile(`(?im)^whois:\s*(\S+)\s*$`).FindStringSubmatch(text); m != nil {
		addr = net.JoinHostPort(m[1], "43")
	}
	c.mu.Lock()
	if c.whois == nil {
		c.whois = map[string]whoisServer{}
	}
	c.whois[tld] = whoisServer{addr: addr, fetched: time.Now()}
	c.mu.Unlock()
	return addr, nil
}

// whoisQuery sends query to a WHOIS server (RFC 3912) and returns its answer.
func (c *Client) whoisQuery(ctx context.Context, addr, query string) (string, error) {
	conn, err := c.Dial(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := io.WriteString(conn, query+"\r\n"); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(bufio.NewReader(conn), maxResponse))
	if err != nil && len(b) == 0 {
		return "", err
	}
	return string(b), nil
}

var (
	// Phrases registries use for names that are not registered. Matched
	// per line, so that legal notices mentioning them do not count.
	whoisFree = regexp.MustCompile(`(?im)^\s*(?:%+\s*|>>>\s*)?(?:` +
		`no match(?:es)?(?: for)?\b.*|` +
		`not found\.?.*|` +
		`no entries found.*|` +
		`no data found.*|` +
		`no object found.*|` +
		`(?:the )?domain(?: name)? (?:\S+ )?(?:was |is )?not found.*|` +
		`no information available about domain name.*|` +
		`nothing found.*|` +
		`status:\s*(?:available|free|not registered)\s*|` +
		`(?:domain )?\S+ is (?:available|free)(?: for registration)?\.?\s*` +
		`)$`)
	whoisLimited  = regexp.MustCompile(`(?i)(?:limit exceeded|rate limit|too many (?:queries|requests)|quota exceeded|try again later)`)
	whoisStatus   = regexp.MustCompile(`(?im)^\s*(?:domain )?status:\s*(.+?)\s*$`)
	whoisExpires  = regexp.MustCompile(`(?im)^\s*(?:registry expiry date|registrar registration expiration date|expir(?:y|ation|es)(?: date| on)?|paid-till|renewal date|valid until):\s*(.+?)\s*$`)
	whoisCreated  = regexp.MustCompile(`(?im)^\s*(?:creation date|created(?: on)?|registered(?: on)?|registration (?:date|time)):\s*(.+?)\s*$`)
	whoisRegistar = regexp.MustCompile(`(?im)^\s*registrar(?: name)?:[ \t]*(.*?)\s*$`)
	whoisName     = regexp.MustCompile(`(?im)^\s*(?:name|organisation|organization):\s*(.+?)\s*$`)
)

func (c *Client) queryWhois(ctx context.Context, addr, name string) (Result, error) {
	text, err := c.whoisQuery(ctx, addr, name)
	host, _, _ := net.SplitHostPort(addr)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", host, err)
	}
	r, err := parseWhois(text)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", host, err)
	}
	r.Source, r.Server = "whois", host
	return r, nil
}

func parseWhois(text string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, fmt.Errorf("empty response")
	}
	if whoisFree.MatchString(text) {
		return Result{Status: Available}, nil
	}
	r := Result{Status: Registered}
	for _, m := range whoisStatus.FindAllStringSubmatch(text, -1) {
		// "clientTransferProhibited https://icann.org/epp#..." -> the code.
		s := strings.Fields(m[1])[0]
		if strings.EqualFold(s, "not") || strings.EqualFold(s, "available") {
			// .be: "Status: NOT AVAILABLE" means registered.
			continue
		}
		r.Statuses = append(r.Statuses, strings.ToLower(s))
		if dropping(s) || strings.Contains(strings.ToLower(m[1]), "redemption") {
			r.Status = Dropping
		}
	}
	// Only now: a rate-limit notice can come with an otherwise empty record.
	if len(r.Statuses) == 0 && whoisLimited.MatchString(text) && !whoisRegistar.MatchString(text) {
		return Result{}, fmt.Errorf("rate limited, try a longer check interval")
	}
	if m := whoisExpires.FindStringSubmatch(text); m != nil {
		r.Expires = parseDate(m[1])
	}
	if m := whoisCreated.FindStringSubmatch(text); m != nil {
		r.Created = parseDate(m[1])
	}
	if loc := whoisRegistar.FindStringSubmatchIndex(text); loc != nil {
		r.Registrar = text[loc[2]:loc[3]]
		// Some registries put the name on the following lines:
		// "Registrar:\n\tName: Example Registrar".
		if r.Registrar == "" {
			rest := text[loc[1]:]
			if end := strings.Index(rest, "\n\n"); end >= 0 {
				rest = rest[:end]
			}
			if m := whoisName.FindStringSubmatch(rest); m != nil {
				r.Registrar = m[1]
			}
		}
	}
	return r, nil
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006.01.02",
	"2006/01/02",
	"02.01.2006",
	"02-Jan-2006",
	"02-January-2006",
	"Mon Jan 2 2006",
	"Mon Jan 2 15:04:05 2006",
	"January 2 2006",
	"2 January 2006",
}

// parseDate parses the date formats seen in WHOIS records; it returns the
// zero time when it does not recognise one.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "UTC"))
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, ",", "")), " ")
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	// Try the leading date of "2026-01-02 (YYYY-MM-DD)" and similar.
	if f := strings.Fields(s); len(f) > 1 {
		return parseDate(f[0])
	}
	return time.Time{}
}
