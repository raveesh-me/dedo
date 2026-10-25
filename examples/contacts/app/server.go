package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"connectrpc.com/connect/v2"

	contactsv1 "github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1"
	"github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1/contactsv1connect"
)

const (
	maxPageSize = 100
	// everyLocation is AIP-159's wildcard parent: a read across every
	// location the caller may see.
	everyLocation = "locations/-"
)

// Server implements the ContactService and the LocationService over an
// in-memory store. It holds no policy code: scopes, tenants, headers and
// error audit come from the interceptor. It only narrows the reads that
// span tenants, which the interceptor cannot, to the caller's locations.
type Server struct {
	store *store
}

var (
	_ contactsv1connect.ContactServiceHandler  = (*Server)(nil)
	_ contactsv1connect.LocationServiceHandler = (*Server)(nil)
)

// NewServer returns a server with the seed locations and no contacts.
func NewServer() *Server { return &Server{store: newStore()} }

// GetContact returns one contact.
func (s *Server) GetContact(_ context.Context, req *contactsv1.GetContactRequest) (*contactsv1.GetContactResponse, error) {
	contact, ok := s.store.get(req.GetName())
	if !ok {
		return nil, contactNotFound(req.GetName())
	}
	return &contactsv1.GetContactResponse{Contact: contact}, nil
}

// ListContacts returns a page of a location's contacts, or with parent
// locations/- of every location the caller may see. The page token is the
// offset of the next contact.
func (s *Server) ListContacts(ctx context.Context, req *contactsv1.ListContactsRequest) (*contactsv1.ListContactsResponse, error) {
	offset, size, err := pageOf(req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	parent := req.GetParent()
	match := func(location string) bool { return location == parent }
	if parent == everyLocation {
		match = func(location string) bool { return visible(ctx, location) }
	}
	contacts, more := s.store.list(match, offset, size)
	res := &contactsv1.ListContactsResponse{Contacts: contacts}
	if more {
		res.NextPageToken = strconv.Itoa(offset + len(contacts))
	}
	return res, nil
}

// CreateContact adds a contact to a location.
func (s *Server) CreateContact(_ context.Context, req *contactsv1.CreateContactRequest) (*contactsv1.CreateContactResponse, error) {
	contact, err := s.store.create(req.GetParent(), req.GetContactId(), req.GetContact())
	switch {
	case errors.Is(err, errNameTaken):
		return nil, apiError(connect.CodeAlreadyExists, reasonAlreadyExists, "contact ID is taken")
	case errors.Is(err, errEmailTaken):
		return nil, apiError(connect.CodeAlreadyExists, reasonEmailTaken, "another contact of this location has the email")
	case err != nil:
		return nil, err
	}
	return &contactsv1.CreateContactResponse{Contact: contact}, nil
}

// UpdateContact changes the fields named in the update mask. Without a mask
// it changes the fields that are set, as AIP-134 says, so an omitted field
// is never wiped.
func (s *Server) UpdateContact(_ context.Context, req *contactsv1.UpdateContactRequest) (*contactsv1.UpdateContactResponse, error) {
	patch := req.GetContact()
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		paths = populated(patch)
	}
	contact, ok := s.store.update(patch, paths)
	if !ok {
		return nil, contactNotFound(patch.GetName())
	}
	return &contactsv1.UpdateContactResponse{Contact: contact}, nil
}

// GetLocation returns one location.
func (s *Server) GetLocation(_ context.Context, req *contactsv1.GetLocationRequest) (*contactsv1.GetLocationResponse, error) {
	location, ok := s.store.location(req.GetName())
	if !ok {
		return nil, locationNotFound(req.GetName())
	}
	return &contactsv1.GetLocationResponse{Location: location}, nil
}

// ListLocations returns a page of the caller's locations. It is
// cross_tenant, so the interceptor checks no tenant, and the filter here is
// all that keeps other locations out.
func (s *Server) ListLocations(ctx context.Context, req *contactsv1.ListLocationsRequest) (*contactsv1.ListLocationsResponse, error) {
	offset, size, err := pageOf(req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	locations, more := s.store.listLocations(func(name string) bool { return visible(ctx, name) }, offset, size)
	res := &contactsv1.ListLocationsResponse{Locations: locations}
	if more {
		res.NextPageToken = strconv.Itoa(offset + len(locations))
	}
	return res, nil
}

// visible reports whether the caller may see a location, such as
// locations/1.
func visible(ctx context.Context, location string) bool {
	id, ok := strings.CutPrefix(location, "locations/")
	return ok && TenantAccess(ctx, id)
}

// pageOf returns the offset a page token names and the page size to use.
func pageOf(token string, pageSize int32) (offset, size int, err error) {
	if token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 {
			return 0, 0, invalidPageToken()
		}
		offset = n
	}
	size = int(pageSize)
	if size <= 0 || size > maxPageSize {
		size = maxPageSize
	}
	return offset, size, nil
}

// populated returns the mutable fields set in a contact: the implied update
// mask of AIP-134.
func populated(contact *contactsv1.Contact) []string {
	var paths []string
	if contact.GetDisplayName() != "" {
		paths = append(paths, "display_name")
	}
	if contact.GetEmail() != "" {
		paths = append(paths, "email")
	}
	if contact.GetHomeLocation() != "" {
		paths = append(paths, "home_location")
	}
	return paths
}
