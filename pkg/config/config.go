package config

import (
	"encoding/json"

	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	libclient "github.com/tendermint/tendermint/rpc/jsonrpc/client"
	"gopkg.in/yaml.v2"
)

// Config represents the configuration for the given application
type Config struct {
	RPCAddr            string `json:"rpc-addr" yaml:"rpc-addr"`
	ChainID            string `json:"chain-id" yaml:"chain-id"`
	BuildRepo          string `json:"build-repo" yaml:"build-repo"`
	BuildCommand       string `json:"build-command" yaml:"build-command"`
	BinaryName         string `json:"binary-name" yaml:"binary-name"`
	BuildVersion       string `json:"build-version" yaml:"build-version"`
	GithubAccessToken  string `json:"github-access-token" yaml:"github-access-token"`
	RegistryRoot       string `json:"registry-root" yaml:"registry-root"`
	RegistryRepo       string `json:"registry-repo" yaml:"registry-repo"`
	RegistryRepoBranch string `json:"registry-repo-branch" yaml:"registry-repo-branch"`
	GitName            string `json:"git-name" yaml:"git-name"`
	GitEmail           string `json:"git-email" yaml:"git-email"`
	CommitMessage      string `json:"commit-message" yaml:"commit-message"`
	Workspace          string `json:"workspace" yaml:"workspace"`
}

// Binary returns the binary file representation from the config
func (c *Config) Binary() []byte {
	// TODO: ensure this is sorted?
	out, _ := json.MarshalIndent(&Binary{
		Name:    c.BinaryName,
		Repo:    c.BuildRepo,
		Build:   c.BuildCommand,
		Version: c.BuildVersion,
	}, "", "  ")
	return out
}

// Client returns a tendermint client to work against the configured chain
func (c *Config) Client() (*rpchttp.HTTP, error) {
	httpClient, err := libclient.DefaultHTTPClient(c.RPCAddr)
	if err != nil {
		return nil, err
	}

	rpcClient, err := rpchttp.NewWithClient(c.RPCAddr, "/websocket", httpClient)
	if err != nil {
		return nil, err
	}

	return rpcClient, nil
}

// YAML converts the config into yaml bytes
func (c *Config) YAML() ([]byte, error) {
	return yaml.Marshal(c)
}

// MustYAML converts to yaml bytes panicing on error
func (c *Config) MustYAML() []byte {
	out, err := c.YAML()
	if err != nil {
		panic(err)
	}
	return out
}

// Binary is everything you need to build the binary
// for the network from the repo configured
type Binary struct {
	Name    string `json:"name"`
	Repo    string `json:"repo"`
	Build   string `json:"build"`
	Version string `json:"version"`
}
