package node

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	registrar "github.com/jackzampolin/cosmos-registrar/pkg/config"
	"github.com/tendermint/tendermint/libs/log"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	tmtypes "github.com/tendermint/tendermint/types"
	"golang.org/x/sync/errgroup"
)

var (
	gen     *ctypes.ResultGenesis
	commit  *ctypes.ResultCommit
	netInfo *ctypes.ResultNetInfo
	rdir    repoDir
	eg      errgroup.Group
)

// DumpInfo connect to ad node and dumps the info about
// that chain into a folder
func DumpInfo(base, chainID string, config *registrar.Config, logger log.Logger) (err error) {
	client, err := config.Client()
	if err != nil {
		return fmt.Errorf("error creating tendermint client: %s", err)
	}

	stat, err := client.Status()
	switch {
	case err != nil:
		return fmt.Errorf("error fetching client status: %s", err)
	case stat.NodeInfo.Network != chainID:
		return fmt.Errorf("node(%s) is on chain(%s) not configured chain(%s)", config.RPCAddr, stat.NodeInfo.Network, chainID)
	case stat.SyncInfo.CatchingUp:
		return fmt.Errorf("node(%s) on chain(%s) still catching up", config.RPCAddr, chainID)
	default:
		logger.Info("GET /status", "rpc-addr", config.RPCAddr)
	}

	eg.Go(func() error {
		gen, err = client.Genesis()
		if err != nil {
			return fmt.Errorf("genesis file: %s", err)
		}
		logger.Info("GET /genesis", "rpc-addr", config.RPCAddr)
		return nil
	})

	eg.Go(func() error {
		h := stat.SyncInfo.LatestBlockHeight
		commit, err = client.Commit(&h)
		if err != nil {
			return fmt.Errorf("commit: %s", err)
		}
		logger.Info(fmt.Sprintf("GET /commit?height=%d", h), "rpc-addr", config.RPCAddr)
		return nil
	})

	// TODO: in a more advanced version of this tool,
	// this would crawl the network a couple of hops
	// and find more peers
	eg.Go(func() error {
		netInfo, err = client.NetInfo()
		if err != nil {
			return fmt.Errorf("net-info: %s", err)
		}
		logger.Info("GET /net_info", "rpc-addr", config.RPCAddr)
		return nil
	})

	if err = eg.Wait(); err != nil {
		return fmt.Errorf("fetching: %s", err)
	}
	// fetch data
	rdir := repoDir{base, chainID}
	if err = createDirIfNotExist(rdir.chainPath(), logger); err != nil {
		return
	}
	if err = createDirIfNotExist(rdir.lrpath(), logger); err != nil {
		return
	}
	// TODO: sanity checks on the genesis file returned from the chain compared with repo
	eg.Go(updateFileGo(rdir.latestPath(), NewLightRoot(commit.SignedHeader), logger))
	eg.Go(updateFileGo(rdir.heightPath(commit.SignedHeader.Header.Height), NewLightRoot(commit.SignedHeader), logger))
	eg.Go(updateFileGo(rdir.binariesPath(), config.Binary(), logger))
	eg.Go(func() error {
		if _, err = os.Stat(rdir.genesisPath()); os.IsNotExist(err) {
			sum, write, err := sortedGenesis(gen.Genesis)
			if err != nil {
				return fmt.Errorf("sorting genesis file: %s", err)
			}

			if err = writeFile(rdir.genesisSumPath(), []byte(sum), logger); err != nil {
				return err
			}
			if err = writeFile(rdir.genesisPath(), write, logger); err != nil {
				return err
			}
		}
		return nil
	})
	eg.Go(func() error {
		qp := stringsFromPeers(netInfo.Peers)
		if _, err = os.Stat(rdir.peersPath()); os.IsNotExist(err) {
			logger.Info("no peers file, popoulating from /net_info", "num", len(qp))
			out, err := json.MarshalIndent(qp, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling peers: %s", err)
			}
			return writeFile(rdir.peersPath(), out, logger)
		}

		var fp []string
		pf, err := os.Open(rdir.peersPath())
		if err != nil {
			return fmt.Errorf("opening peer file: %s", err)
		}
		pfb, err := ioutil.ReadAll(pf)
		if err != nil {
			pf.Close()
			return fmt.Errorf("reading peer file: %s", err)
		}
		if err = json.Unmarshal(pfb, &fp); err != nil {
			pf.Close()
			return fmt.Errorf("unmarshaling peer strings: %s", err)
		}
		pf.Close()
		ps := dedupe(append(fp, qp...))
		// TODO: we should check peer liveness here
		logger.Info(fmt.Sprintf("added %d new peers to %s", len(ps)-len(fp), path.Base(rdir.peersPath())))
		w, err := json.MarshalIndent(ps, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling peers: %s", err)
		}
		return updateFile(rdir.peersPath(), w, logger)
	})

	if err = eg.Wait(); err != nil {
		return err
	}
	return
}

type repoDir struct {
	dir     string
	chainID string
}

func (r repoDir) chainPath() string         { return path.Join(r.dir, r.chainID) }
func (r repoDir) genesisPath() string       { return path.Join(r.chainPath(), "genesis.json") }
func (r repoDir) genesisSumPath() string    { return path.Join(r.chainPath(), "genesis.json.sum") }
func (r repoDir) lrpath() string            { return path.Join(r.chainPath(), "light-roots") }
func (r repoDir) latestPath() string        { return path.Join(r.lrpath(), "latest.json") }
func (r repoDir) heightPath(h int64) string { return path.Join(r.lrpath(), fmt.Sprintf("%d.json", h)) }
func (r repoDir) binariesPath() string      { return path.Join(r.chainPath(), "binaries.json") }
func (r repoDir) peersPath() string         { return path.Join(r.chainPath(), "peers.json") }

func updateFileGo(pth string, payload []byte, log log.Logger) func() error {
	return func() (err error) {
		return updateFile(pth, payload, log)
	}
}

func updateFile(pth string, payload []byte, log log.Logger) error {
	log.Info(fmt.Sprintf("deleting pth %s", path.Base(pth)))
	os.Remove(pth)
	return writeFile(pth, payload, log)
}

func writeFile(pth string, payload []byte, log log.Logger) (err error) {
	log.Info(fmt.Sprintf("writing pth %s", path.Base(pth)))
	if err = ioutil.WriteFile(pth, payload, 0644); err != nil {
		return fmt.Errorf("writing %s: %s", pth, err)
	}
	return nil
}

func createDirIfNotExist(pth string, log log.Logger) (err error) {
	if _, err = os.Stat(pth); os.IsNotExist(err) {
		log.Info("creating directory", "dir", path.Base(pth))
		if err = os.Mkdir(pth, os.ModePerm); err != nil {
			return fmt.Errorf("making dir %s: %s", pth, err)
		}
	}
	return nil
}

func stringsFromPeers(ni []ctypes.Peer) (qp []string) {
	for _, p := range ni {
		port := strings.Split(p.NodeInfo.ListenAddr, ":")
		qp = append(qp, fmt.Sprintf("%s@%s:%s", p.NodeInfo.ID(), p.RemoteIP, port[len(port)-1]))
	}
	return
}

func sortedGenesis(gen *tmtypes.GenesisDoc) (sum string, indented []byte, err error) {
	// prepare to sort
	if indented, err = json.Marshal(gen); err != nil {
		return
	}

	// sort
	var c interface{}
	if err = json.Unmarshal(indented, &c); err != nil {
		return
	}

	// indent
	if indented, err = json.MarshalIndent(c, "", "  "); err != nil {
		return
	}

	// sum
	sum = fmt.Sprintf("%x", sha256.Sum256(indented))
	return
}

// LightRoot is the format for a light client root file which
// will be used for state sync
type LightRoot struct {
	TrustHeight int64  `json:"trust-height"`
	TrustHash   string `json:"trust-hash"`
}

// NewLightRoot returns a new light root
func NewLightRoot(sh tmtypes.SignedHeader) []byte {
	out, _ := json.MarshalIndent(&LightRoot{
		TrustHeight: sh.Header.Height,
		TrustHash:   sh.Commit.BlockID.Hash.String(),
	}, "", "  ")
	return out
}

func dedupe(ele []string) (out []string) {
	e := map[string]bool{}
	for v := range ele {
		e[ele[v]] = true
	}
	for k := range e {
		out = append(out, k)
	}
	return
}
