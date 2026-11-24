package state

import (
	"time"
)

type Database struct {
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Version       string    `json:"version"`
	Preset        string    `json:"preset,omitempty"`
	CustomImage   string    `json:"custom_image,omitempty"`
	Group         string    `json:"group,omitempty"`
	Project       string    `json:"project,omitempty"`
	ContainerName string    `json:"container_name"`
	ContainerID   string    `json:"container_id"`
	Port          int       `json:"port"`
	RootPassword  string    `json:"root_password"`
	DatabaseName  string    `json:"database_name"`
	User          string    `json:"user,omitempty"`
	UserPassword  string    `json:"user_password,omitempty"`
	VolumeName    string    `json:"volume_name,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (d *Database) GetUsername() string {
	if d.User != "" {
		return d.User
	}
	switch d.Provider {
	case "postgres":
		return "postgres"
	case "redis", "valkey":
		return "default"
	default:
		return "root"
	}
}

func (d *Database) GetPassword() string {
	if d.UserPassword != "" {
		return d.UserPassword
	}
	return d.RootPassword
}
