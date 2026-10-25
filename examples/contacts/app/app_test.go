package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/raveesh-me/dedo/examples/contacts/app"
	contactsv1 "github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1"
	"github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1/contactsv1connect"
)

type env struct {
	url string
	log *bytes.Buffer
}

func newEnv(t *testing.T) env {
	t.Helper()
	var log bytes.Buffer
	handler, err := app.NewHandler(app.Config{
		Log:           slog.New(slog.NewTextHandler(&log, nil)),
		EnforceScopes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return env{url: srv.URL, log: &log}
}

func (e env) connectClient(token string) *connect.Client {
	return connect.NewClient(connecthttp.NewTransport(http.DefaultClient, e.url), bearerAuth(token))
}

// contacts returns a ContactService client that sends the token on every
// call.
func (e env) contacts(token string) contactsv1connect.ContactServiceClient {
	return contactsv1connect.NewContactServiceClient(e.connectClient(token))
}

// locations returns a LocationService client that sends the token on every
// call.
func (e env) locations(token string) contactsv1connect.LocationServiceClient {
	return contactsv1connect.NewLocationServiceClient(e.connectClient(token))
}

// bearerAuth sends the token on every call.
func bearerAuth(token string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			info, ok := connect.CallInfoForClientContext(ctx)
			if !ok {
				ctx, info = connect.NewClientContext(ctx)
			}
			info.RequestHeader().Set("Authorization", "Bearer "+token)
			return next(ctx, spec)
		}
	}
}

// rest sends a REST request and returns the response and its body.
func (e env) rest(t *testing.T, method, path, token string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, e.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(b)
}

// create adds a contact as the token's caller, and fails the test if it
// cannot.
func create(t *testing.T, e env, token, parent, id string, contact *contactsv1.Contact) *contactsv1.Contact {
	t.Helper()
	res, err := e.contacts(token).CreateContact(context.Background(), &contactsv1.CreateContactRequest{
		Parent: parent, ContactId: id, Contact: contact,
	})
	if err != nil {
		t.Fatalf("create %s/contacts/%s: %v", parent, id, err)
	}
	return res.GetContact()
}

// reason returns the ErrorInfo reason of a Connect error.
func reason(err error) string {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		return ""
	}
	for _, d := range cerr.Details() {
		if info, derr := connectproto.UnmarshalErrorDetail(d); derr == nil {
			if info, ok := info.(*errdetails.ErrorInfo); ok {
				return info.GetReason()
			}
		}
	}
	return ""
}

// A caller creates, reads, lists and updates contacts in its own location,
// with the AIP semantics and the error reasons the contract declares.
func TestCRUD(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.contacts(app.Loc1Writer)

	generated := create(t, e, app.Loc1Writer, "locations/1", "", &contactsv1.Contact{DisplayName: "Ada"})
	if generated.GetName() != "locations/1/contacts/1" {
		t.Errorf("generated name = %q", generated.GetName())
	}
	create(t, e, app.Loc1Writer, "locations/1", "a", &contactsv1.Contact{DisplayName: "Grace", Email: "grace@example.com"})

	_, err := w.CreateContact(ctx, &contactsv1.CreateContactRequest{Parent: "locations/1", ContactId: "a", Contact: &contactsv1.Contact{DisplayName: "dup"}})
	if connect.CodeOf(err) != connect.CodeAlreadyExists || reason(err) != "CONTACT_ALREADY_EXISTS" {
		t.Errorf("duplicate ID: %v (reason %q)", err, reason(err))
	}
	_, err = w.CreateContact(ctx, &contactsv1.CreateContactRequest{Parent: "locations/1", Contact: &contactsv1.Contact{DisplayName: "x", Email: "grace@example.com"}})
	if connect.CodeOf(err) != connect.CodeAlreadyExists || reason(err) != "EMAIL_TAKEN" {
		t.Errorf("taken email: %v (reason %q)", err, reason(err))
	}

	got, err := w.GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/1/contacts/a"})
	if err != nil || got.GetContact().GetEmail() != "grace@example.com" {
		t.Fatalf("get: %v %v", err, got.GetContact())
	}
	_, err = w.GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/1/contacts/nope"})
	if connect.CodeOf(err) != connect.CodeNotFound || reason(err) != "CONTACT_NOT_FOUND" {
		t.Errorf("get missing: %v (reason %q)", err, reason(err))
	}

	masked, err := w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{
		Contact:    &contactsv1.Contact{Name: "locations/1/contacts/a", DisplayName: "ignored", Email: "g@example.com"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}},
	})
	if err != nil || masked.GetContact().GetDisplayName() != "Grace" || masked.GetContact().GetEmail() != "g@example.com" {
		t.Errorf("masked update: %v %v", err, masked.GetContact())
	}
	// Without a mask, the fields that are set change and the others stay
	// (AIP-134).
	implied, err := w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{
		Contact: &contactsv1.Contact{Name: "locations/1/contacts/a", HomeLocation: "locations/1"},
	})
	if err != nil || implied.GetContact().GetHomeLocation() != "locations/1" || implied.GetContact().GetEmail() != "g@example.com" {
		t.Errorf("unmasked update: %v %v", err, implied.GetContact())
	}
	_, err = w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{Contact: &contactsv1.Contact{Name: "locations/1/contacts/nope", DisplayName: "x"}})
	if connect.CodeOf(err) != connect.CodeNotFound || reason(err) != "CONTACT_NOT_FOUND" {
		t.Errorf("update missing: %v (reason %q)", err, reason(err))
	}

	first, err := w.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/1", PageSize: 1})
	if err != nil || len(first.GetContacts()) != 1 || first.GetNextPageToken() == "" {
		t.Fatalf("first page: %v %v", err, first)
	}
	second, err := w.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/1", PageSize: 1, PageToken: first.GetNextPageToken()})
	if err != nil || len(second.GetContacts()) != 1 || second.GetNextPageToken() != "" || second.GetContacts()[0].GetName() != "locations/1/contacts/a" {
		t.Errorf("second page: %v %v", err, second)
	}
	_, err = w.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/1", PageToken: "x"})
	if connect.CodeOf(err) != connect.CodeInvalidArgument || reason(err) != "INVALID_PAGE_TOKEN" {
		t.Errorf("bad page token: %v (reason %q)", err, reason(err))
	}
	// Every error above carries a reason the contract declares.
	if log := e.log.String(); strings.Contains(log, "undeclared error reason") || strings.Contains(log, "error without ErrorInfo") {
		t.Errorf("error audit: %s", log)
	}
}

// A writer of one location gets PERMISSION_DENIED for every call naming
// another, over Connect and REST, whether or not the resource exists.
func TestCrossTenantDenied(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	create(t, e, app.Loc2Writer, "locations/2", "x", &contactsv1.Contact{DisplayName: "theirs"})
	w := e.contacts(app.Loc1Writer)

	calls := map[string]error{}
	_, calls["get existing"] = w.GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/2/contacts/x"})
	_, calls["get missing"] = w.GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/2/contacts/nope"})
	_, calls["list"] = w.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/2"})
	_, calls["list every location"] = w.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/-"})
	_, calls["create"] = w.CreateContact(ctx, &contactsv1.CreateContactRequest{Parent: "locations/2", Contact: &contactsv1.Contact{DisplayName: "x"}})
	_, calls["update"] = w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{Contact: &contactsv1.Contact{Name: "locations/2/contacts/x", DisplayName: "mine now"}})
	_, calls["get location"] = e.locations(app.Loc1Writer).GetLocation(ctx, &contactsv1.GetLocationRequest{Name: "locations/2"})
	for call, err := range calls {
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("%s: %v, want permission_denied", call, err)
		}
	}
	if res, body := e.rest(t, http.MethodGet, "/commerce/contacts/v1/locations/2/contacts/x", app.Loc1Writer); res.StatusCode != http.StatusForbidden {
		t.Errorf("REST get: %d %s", res.StatusCode, body)
	}
	got, err := e.contacts(app.Loc2Writer).GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/2/contacts/x"})
	if err != nil || got.GetContact().GetDisplayName() != "theirs" {
		t.Errorf("the denied update changed the contact: %v %v", err, got.GetContact())
	}
}

// A resource reference nested in the request, contact.home_location, is
// checked like the tenant field: it may name only the caller's locations.
func TestNestedReference(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	create(t, e, app.Loc1Writer, "locations/1", "a", &contactsv1.Contact{DisplayName: "Ada"})
	w := e.contacts(app.Loc1Writer)

	_, err := w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{
		Contact:    &contactsv1.Contact{Name: "locations/1/contacts/a", HomeLocation: "locations/2"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"home_location"}},
	})
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("update naming locations/2: %v, want permission_denied", err)
	}
	_, err = w.CreateContact(ctx, &contactsv1.CreateContactRequest{
		Parent: "locations/1", Contact: &contactsv1.Contact{DisplayName: "x", HomeLocation: "locations/2"},
	})
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("create naming locations/2: %v, want permission_denied", err)
	}
	_, err = w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{
		Contact: &contactsv1.Contact{Name: "locations/1/contacts/a", HomeLocation: "locations/1/../2"},
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("update with a malformed reference: %v, want invalid_argument", err)
	}
	updated, err := w.UpdateContact(ctx, &contactsv1.UpdateContactRequest{
		Contact:    &contactsv1.Contact{Name: "locations/1/contacts/a", HomeLocation: "locations/1"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"home_location"}},
	})
	if err != nil || updated.GetContact().GetHomeLocation() != "locations/1" {
		t.Errorf("update naming locations/1: %v %v", err, updated.GetContact())
	}
}

// AllReader reads every location, the locations/- wildcard included, and
// writes none.
func TestAllReader(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	create(t, e, app.Loc1Writer, "locations/1", "a", &contactsv1.Contact{DisplayName: "one"})
	create(t, e, app.Loc2Writer, "locations/2", "b", &contactsv1.Contact{DisplayName: "two"})
	r := e.contacts(app.AllReader)

	for _, name := range []string{"locations/1/contacts/a", "locations/2/contacts/b"} {
		if _, err := r.GetContact(ctx, &contactsv1.GetContactRequest{Name: name}); err != nil {
			t.Errorf("get %s: %v", name, err)
		}
	}
	every, err := r.ListContacts(ctx, &contactsv1.ListContactsRequest{Parent: "locations/-"})
	if err != nil || len(every.GetContacts()) != 2 {
		t.Errorf("list locations/-: %v %v", err, every)
	}
	_, err = r.CreateContact(ctx, &contactsv1.CreateContactRequest{Parent: "locations/1", Contact: &contactsv1.Contact{DisplayName: "x"}})
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("create: %v, want permission_denied", err)
	}
	_, err = r.UpdateContact(ctx, &contactsv1.UpdateContactRequest{Contact: &contactsv1.Contact{Name: "locations/1/contacts/a", DisplayName: "x"}})
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("update: %v, want permission_denied", err)
	}
	_, err = e.locations(app.AllReader).GetLocation(ctx, &contactsv1.GetLocationRequest{Name: "locations/9"})
	if connect.CodeOf(err) != connect.CodeNotFound || reason(err) != "LOCATION_NOT_FOUND" {
		t.Errorf("get missing location: %v (reason %q)", err, reason(err))
	}
}

// ListLocations is cross_tenant: the scope guards it, and it returns only
// the caller's own locations.
func TestListLocationsListsOwnLocations(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		token string
		want  []string
	}{
		{app.Loc1Reader, []string{"locations/1"}},
		{app.Loc1Writer, []string{"locations/1"}},
		{app.Loc2Writer, []string{"locations/2"}},
		{app.AllReader, []string{"locations/1", "locations/2", "locations/3"}},
	}
	for _, tt := range tests {
		res, err := e.locations(tt.token).ListLocations(context.Background(), &contactsv1.ListLocationsRequest{})
		if err != nil {
			t.Errorf("%s: %v", tt.token, err)
			continue
		}
		var got []string
		for _, l := range res.GetLocations() {
			got = append(got, l.GetName())
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: locations %v, want %v", tt.token, got, tt.want)
		}
	}
	paged, err := e.locations(app.AllReader).ListLocations(context.Background(), &contactsv1.ListLocationsRequest{PageSize: 2})
	if err != nil || len(paged.GetLocations()) != 2 || paged.GetNextPageToken() != "2" {
		t.Errorf("first page of two: %v %v", err, paged)
	}
}

// A missing or unknown token holds no scopes and no tenants.
func TestUnknownTokenGetsNothing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, token := range []string{"", "nobody"} {
		_, err := e.contacts(token).GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/1/contacts/a"})
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("token %q get: %v, want permission_denied", token, err)
		}
		_, err = e.locations(token).ListLocations(ctx, &contactsv1.ListLocationsRequest{})
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("token %q list locations: %v, want permission_denied", token, err)
		}
	}
	// A reader with the scope but not the tenant gets the same answer.
	_, err := e.contacts(app.Loc1Reader).GetContact(ctx, &contactsv1.GetContactRequest{Name: "locations/3/contacts/a"})
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("Loc1Reader get in locations/3: %v", err)
	}
}
