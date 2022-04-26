package users

import (
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube/naming"
	"github.com/shuttlerock/devops-api/api/v1alpha1"
)

type UserDetailService struct {
	cache map[string]*v1alpha1.UserDetails
}

func (s *UserDetailService) GetUser(login string) *v1alpha1.UserDetails {
	if s.cache == nil {
		s.cache = map[string]*v1alpha1.UserDetails{}
	}
	return s.cache[login]
}

func (s *UserDetailService) CreateOrUpdateUser(u *v1alpha1.UserDetails) error {
	if u == nil || u.Login == "" {
		return nil
	}

	id := naming.ToValidName(u.Login)

	// check for an existing user by email
	existing := s.GetUser(id)
	if existing == nil {
		s.cache[id] = u
		return nil
	}
	if u.Email != "" {
		existing.Email = u.Email
	}
	if u.AvatarURL != "" {
		existing.AvatarURL = u.AvatarURL
	}
	if u.URL != "" {
		existing.URL = u.URL
	}
	if u.Name != "" {
		existing.Name = u.Name
	}
	if u.Login != "" {
		existing.Login = u.Login
	}
	return nil
}
