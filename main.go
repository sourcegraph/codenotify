package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var verbose io.Writer = os.Stderr

func main() {
	if err := testableMain(os.Stdout, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)

		if isRateLimitErr(err) {
			fmt.Println("Github API limit reached. Soft exiting")
		} else {
			os.Exit(1)
		}
	}
}

func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "API rate limit exceeded")
}

func testableMain(stdout io.Writer, args []string) error {
	opts, err := getOptions(stdout, args)
	if err != nil {
		return err
	}

	if opts == nil {
		return nil
	}

	commits := opts.baseRef + "..." + opts.headRef
	diff, err := run("git", "-C", opts.cwd, "diff", "--name-only", commits)
	if err != nil {
		return fmt.Errorf("error diffing %s: %w", commits, err)
	}

	paths, err := readLines(diff)
	if err != nil {
		return fmt.Errorf("error scanning lines from diff: %s\n%s", err, string(diff))
	}

	notifs, err := notifications(&gitfs{cwd: opts.cwd, rev: opts.baseRef}, paths, opts.filename)
	if err != nil {
		return err
	}

	if opts.author != "" {
		fmt.Fprintf(verbose, "not notifying pull request author %s\n", opts.author)
		delete(notifs, opts.author)
	}

	return opts.print(notifs)
}

func run(command string, args ...string) ([]byte, error) {
	out, err := exec.Command(command, args...).CombinedOutput()
	cmd := strings.Join(append([]string{command}, args...), " ")
	if err != nil {
		return nil, fmt.Errorf("error running command: %s -> %w", cmd, err)
	}
	fmt.Fprintln(verbose, cmd)
	fmt.Fprintln(verbose, string(out))
	return out, nil
}

func getOptions(stdout io.Writer, args []string) (*options, error) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return githubActionOptions()
	}
	return cliOptions(stdout, args)
}

func cliOptions(stdout io.Writer, args []string) (*options, error) {
	flags := flag.NewFlagSet("codenotify", flag.ContinueOnError)
	opts := options{}
	flags.StringVar(&opts.cwd, "cwd", "", "The working directory to use.")
	flags.StringVar(&opts.baseRef, "baseRef", "", "The base ref to use when computing the file diff.")
	flags.StringVar(&opts.headRef, "headRef", "HEAD", "The head ref to use when computing the file diff.")
	flags.StringVar(&opts.author, "author", "", "The author of the diff.")
	flags.StringVar(&opts.format, "format", "text", "The format of the output: text or markdown")
	flags.StringVar(&opts.filename, "filename", "CODENOTIFY", "The filename in which file subscribers are defined")
	flags.IntVar(&opts.subscriberThreshold, "subscriber-threshold", 0, "The threshold of notifying subscribers")
	v := *flags.Bool("verbose", false, "Verbose messages printed to stderr")

	if v {
		verbose = os.Stderr
	} else {
		verbose = ioutil.Discard
	}

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	opts.print = func(notifs map[string][]string) error {
		return opts.writeNotifications(stdout, notifs)
	}
	return &opts, nil
}

type pullRequest struct {
	Base struct {
		Sha string `json:"sha"`
	} `json:"base"`
	Head struct {
		Sha string `json:"sha"`
	} `json:"head"`
	NodeID string `json:"node_id"`
	User   struct {
		Login string `json:"login"`
	} `json:"User"`
	Draft bool `json:"draft"`
}

func githubActionOptions() (*options, error) {
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return nil, fmt.Errorf("env var GITHUB_EVENT_PATH not set")
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read GitHub event json %s: %s", path, err)
	}

	var event struct {
		PullRequest pullRequest `json:"pull_request"`
	}

	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("unable to decode GitHub event: %s\n%s", err, string(data))
	}

	if event.PullRequest.Draft {
		fmt.Fprintln(verbose, "Not sending notifications for draft pull request.")
		return nil, nil
	}

	commitCount, err := commitCount(event.PullRequest.NodeID)
	if err != nil {
		return nil, err
	}

	cwd := os.Getenv("GITHUB_WORKSPACE")
	_, err = run("git", "-C", cwd, "-c", "protocol.version=2", "fetch", "--deepen", strconv.Itoa(commitCount))
	if err != nil {
		return nil, err
	}

	filename := os.Getenv("INPUT_FILENAME")
	if filename == "" {
		return nil, fmt.Errorf("env var INPUT_FILENAME not set")
	}

	subscriberThreshold, _ := strconv.Atoi(os.Getenv("INPUT_SUBSCRIBER-THRESHOLD"))

	o := &options{
		cwd:                 cwd,
		format:              "markdown",
		filename:            filename,
		subscriberThreshold: subscriberThreshold,
		baseRef:             event.PullRequest.Base.Sha,
		headRef:             event.PullRequest.Head.Sha,
		author:              "@" + event.PullRequest.User.Login,
	}
	o.print = commentOnGitHubPullRequest(o, event.PullRequest.NodeID)
	return o, nil
}

func commentOnGitHubPullRequest(o *options, prNodeID string) func(map[string][]string) error {
	return func(notifs map[string][]string) error {
		comment := bytes.Buffer{}
		if err := o.writeNotifications(&comment, notifs); err != nil {
			return err
		}

		id, err := existingCommentId(prNodeID, o.filename)
		if err != nil {
			return err
		}

		if id == "" {
			if len(notifs) == 0 {
				fmt.Fprintln(verbose, "not adding a comment because there are no notifications to send")
				return nil
			}
			return addComment(prNodeID, comment.String())
		}

		return updateComment(id, comment.String())
	}
}

func updateComment(id, body string) error {
	fmt.Fprintf(verbose, "updating existing comment: %s\n", id)
	return graphql(`
		mutation UpdateComment ($id: ID!, $body: String!) {
			updateIssueComment(input: {
				id: $id
				body: $body
			}) {
				clientMutationId
			}
		}`,
		map[string]interface{}{
			"id":   id,
			"body": body,
		},
		nil,
	)
}

func addComment(subjectId, body string) error {
	fmt.Fprintf(verbose, "adding comment to pr %s\n", subjectId)
	return graphql(`
		mutation AddComment ($subjectId: ID!, $body: String!) {
			addComment(input: {
				subjectId: $subjectId
				body: $body
			}) {
				clientMutationId
			}
		}`,
		map[string]interface{}{
			"subjectId": subjectId,
			"body":      body,
		},
		nil,
	)
}

func commitCount(prNodeID string) (int, error) {
	data := struct {
		Node struct {
			Commits struct {
				TotalCount int `json:"totalCount"`
			} `json:"commits"`
		} `json:"node"`
	}{}
	err := graphql(`
		query CommitCount ($nodeId: ID!) {
			node(id: $nodeId) {
				... on PullRequest {
					commits {
						totalCount
					}
				}
			}
		}`,
		map[string]interface{}{
			"nodeId": prNodeID,
		},
		&data,
	)

	return data.Node.Commits.TotalCount, err
}

func existingCommentId(prNodeID string, filename string) (string, error) {
	data := struct {
		Node struct {
			Comments struct {
				Nodes []struct {
					Id     string `json:"id"`
					Author struct {
						Login string `json:"login"`
					} `json:"author"`
					Body string `json:"body"`
				} `json:"nodes"`
			} `json:"comments"`
		} `json:"node"`
	}{}
	err := graphql(`
		query GetPullRequestComments ($nodeId: ID!) {
			node(id: $nodeId) {
				... on PullRequest {
					comments(first: 100) {
						nodes {
							id
							author {
								login
							}
							body
						}
					}
				}
			}
		}`,
		map[string]interface{}{
			"nodeId": prNodeID,
		},
		&data,
	)
	if err != nil {
		return "", err
	}

	for _, comment := range data.Node.Comments.Nodes {
		if strings.HasPrefix(comment.Body, markdownCommentTitle(filename)) {
			return comment.Id, nil
		}
	}

	return "", nil
}

func graphql(query string, variables map[string]interface{}, responseData interface{}) error {
	reqbody, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal query %s and variables %s: %w", query, variables, err)
	}

	req, err := http.NewRequest(http.MethodPost, os.Getenv("GITHUB_GRAPHQL_URL"), bytes.NewBuffer(reqbody))
	if err != nil {
		return err
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is not set")
	}
	req.Header.Set("Authorization", "bearer "+token)

	reqdump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return fmt.Errorf("error dumping request: %w", err)
	}

	cl := &http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respdump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return fmt.Errorf("error dumping response: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("non-200 response:\n%s\n\nrequest:\n%s", string(respdump), string(reqdump))
	}

	response := struct {
		Data   interface{}
		Errors []struct {
			Type    string   `json:"type"`
			Path    []string `json:"path"`
			Message string   `json:"message"`
		} `json:"errors"`
	}{
		Data: responseData,
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("error decoding json response:\n%s\n%w", respdump, err)
	}

	if len(response.Errors) > 0 {
		return fmt.Errorf("graphql error: %s\nrequest:\n%s", response.Errors[0].Message, reqdump)
	}

	return nil
}

type options struct {
	cwd                 string
	baseRef             string
	headRef             string
	format              string
	filename            string
	subscriberThreshold int
	author              string
	print               func(notifs map[string][]string) error
}

func markdownCommentTitle(filename string) string {
	return fmt.Sprintf("<!-- codenotify:%s report -->\n", filename)
}

func (o *options) writeNotifications(w io.Writer, notifs map[string][]string) error {
	if o.subscriberThreshold > 0 && len(notifs) > o.subscriberThreshold {
		fmt.Fprintf(w, "Not notifying subscribers because the number of notifying subscribers (%d) has exceeded the threshold (%d).\n", len(notifs), o.subscriberThreshold)
		return nil
	}

	subs := make([]string, 0, len(notifs))
	for sub := range notifs {
		subs = append(subs, sub)
	}
	sort.Strings(subs)

	switch o.format {
	case "text":
		fmt.Fprintf(w, "%s...%s\n", o.baseRef, o.headRef)
		if len(notifs) == 0 {
			fmt.Fprintln(w, "No notifications.")
		} else {
			for _, sub := range subs {
				files := notifs[sub]
				fmt.Fprintln(w, sub, "->", strings.Join(files, ", "))
			}
		}
		return nil
	case "markdown":
		fmt.Fprint(w, markdownCommentTitle(o.filename))
		fmt.Fprintf(w, "[Codenotify](https://github.com/sourcegraph/codenotify): Notifying subscribers in %s files for diff %s...%s.\n\n", o.filename, o.baseRef, o.headRef)
		if len(notifs) == 0 {
			fmt.Fprintln(w, "No notifications.")
		} else {
			fmt.Fprint(w, "| Notify | File(s) |\n")
			fmt.Fprint(w, "|-|-|\n")
			for _, sub := range subs {
				files := notifs[sub]
				fmt.Fprintf(w, "| %s | %s |\n", sub, strings.Join(files, "<br>"))
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", o.format)
	}
}

func readLines(b []byte) ([]string, error) {
	lines := []string{}
	scanner := bufio.NewScanner(bytes.NewBuffer(b))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func notifications(fs FS, paths []string, notifyFilename string) (map[string][]string, error) {
	notifications := map[string][]string{}
	for _, path := range paths {
		subs, err := subscribers(fs, path, notifyFilename)
		if err != nil {
			return nil, err
		}

		for _, sub := range subs {
			notifications[sub] = append(notifications[sub], path)
		}
	}

	return notifications, nil
}

func subscribers(fs FS, path string, notifyFilename string) ([]string, error) {
	fmt.Fprintf(verbose, "analyzing subscribers in %s files\n", notifyFilename)
	subscribers := []string{}

	parts := strings.Split(path, string(os.PathSeparator))
	for i := range parts {
		base := filepath.Join(parts[:i]...)
		rulefilepath := filepath.Join(base, notifyFilename)

		rulefile, err := fs.Open(rulefilepath)
		if err != nil {
			if err == os.ErrNotExist {
				continue
			}
			return nil, err
		}

		scanner := bufio.NewScanner(rulefile)
		for scanner.Scan() {
			rule := scanner.Text()
			if rule != "" && rule[0] == '#' {
				// skip comment
				continue
			}

			fields := strings.Fields(rule)
			switch len(fields) {
			case 0:
				// skip blank line
				continue
			case 1:
				return nil, fmt.Errorf("expected at least two fields for rule in %s: %s", rulefilepath, rule)
			}

			rel, err := filepath.Rel(base, path)
			if err != nil {
				return nil, err
			}

			re, err := patternToRegexp(fields[0])
			if err != nil {
				return nil, fmt.Errorf("invalid pattern in %s: %s: %w", rulefilepath, rule, err)
			}

			if re.MatchString(rel) {
				subscribers = append(subscribers, fields[1:]...)
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	return subscribers, nil
}

func patternToRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern[len(pattern)-1:] == "/" {
		pattern += "**"
	}
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, `/\*\*/`, "/([^/]*/)*")
	pattern = strings.ReplaceAll(pattern, `\*\*/`, "([^/]+/)*")
	pattern = strings.ReplaceAll(pattern, `/\*\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\*\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\*`, "[^/]*")
	pattern = "^" + pattern + "$"
	return regexp.Compile(pattern)
}
