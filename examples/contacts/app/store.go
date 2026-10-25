package app

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	contactsv1 "github.com/raveesh-me/dedo/examples/contacts/gen/go/raveesh_me/contacts/v1"
)

// The ways a create can fail.
var (
	errNameTaken  = errors.New("contact name taken")
	errEmailTaken = errors.New("email taken in this location")
)

// seedLocations are the locations every store starts with: the tenants.
var seedLocations = []*contactsv1.Location{
	{Name: "locations/1", DisplayName: "Downtown"},
	{Name: "locations/2", DisplayName: "Airport"},
	{Name: "locations/3", DisplayName: "Harbour"},
}

// store keeps the locations, and their contacts in creation order. It hands
// out clones, so callers can never change stored state.
type store struct {
	mu        sync.Mutex
	locations []*contactsv1.Location
	contacts  map[string]*contactsv1.Contact
	order     []string
	nextID    int
}

func newStore() *store {
	locations := make([]*contactsv1.Location, 0, len(seedLocations))
	for _, l := range seedLocations {
		locations = append(locations, clone(l))
	}
	return &store{locations: locations, contacts: map[string]*contactsv1.Contact{}}
}

func (s *store) location(name string) (*contactsv1.Location, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.locations {
		if l.GetName() == name {
			return clone(l), true
		}
	}
	return nil, false
}

// listLocations returns up to size of the locations that match, from
// offset, and whether more follow.
func (s *store) listLocations(match func(name string) bool, offset, size int) ([]*contactsv1.Location, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []*contactsv1.Location
	for _, l := range s.locations {
		if match(l.GetName()) {
			found = append(found, l)
		}
	}
	return page(found, offset, size)
}

func (s *store) get(name string) (*contactsv1.Contact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	contact, ok := s.contacts[name]
	if !ok {
		return nil, false
	}
	return clone(contact), true
}

// list returns up to size of the contacts whose location matches, from
// offset, and whether more follow.
func (s *store) list(match func(location string) bool, offset, size int) ([]*contactsv1.Contact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []*contactsv1.Contact
	for _, name := range s.order {
		if match(locationOf(name)) {
			found = append(found, s.contacts[name])
		}
	}
	return page(found, offset, size)
}

// create stores a copy of contact in the location under id, or a generated
// ID when id is empty. It fails with errNameTaken when the name is taken,
// and errEmailTaken when another contact of the location has the email.
func (s *store) create(parent, id string, contact *contactsv1.Contact) (*contactsv1.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		s.nextID++
		id = strconv.Itoa(s.nextID)
	}
	name := parent + "/contacts/" + id
	if _, taken := s.contacts[name]; taken {
		return nil, errNameTaken
	}
	if email := contact.GetEmail(); email != "" {
		for _, other := range s.order {
			if locationOf(other) == parent && s.contacts[other].GetEmail() == email {
				return nil, errEmailTaken
			}
		}
	}
	stored := clone(contact)
	stored.Name = name
	s.contacts[name] = stored
	s.order = append(s.order, name)
	return clone(stored), nil
}

// update applies the masked fields of patch to the stored contact.
func (s *store) update(patch *contactsv1.Contact, paths []string) (*contactsv1.Contact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.contacts[patch.GetName()]
	if !ok {
		return nil, false
	}
	next := clone(current)
	for _, path := range paths {
		switch path {
		case "display_name":
			next.DisplayName = patch.GetDisplayName()
		case "email":
			next.Email = patch.GetEmail()
		case "home_location":
			next.HomeLocation = patch.GetHomeLocation()
		}
	}
	s.contacts[next.GetName()] = next
	return clone(next), true
}

// locationOf returns the location of a contact's name: locations/1 for
// locations/1/contacts/2.
func locationOf(contact string) string {
	location, _, _ := strings.Cut(contact, "/contacts/")
	return location
}

// page returns clones of up to size items from offset, and whether more
// follow.
func page[M proto.Message](items []M, offset, size int) ([]M, bool) {
	if offset >= len(items) {
		return nil, false
	}
	end := min(offset+size, len(items))
	out := make([]M, 0, end-offset)
	for _, item := range items[offset:end] {
		out = append(out, clone(item))
	}
	return out, end < len(items)
}

func clone[M proto.Message](m M) M {
	return proto.Clone(m).(M)
}
