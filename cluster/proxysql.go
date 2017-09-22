package cluster

import (
	"fmt"

	"github.com/signal18/replication-manager/proxysql"
)

func (cluster *Cluster) initProxysql(proxy *Proxy) {
	if cluster.conf.ProxysqlOn == false {
		return
	}

	psql := proxysql.ProxySQL{
		User:     proxy.User,
		Password: proxy.Pass,
		Host:     proxy.Host,
		Port:     proxy.Port,
		WriterHG: fmt.Sprintf("%d", proxy.WritePort),
		ReaderHG: fmt.Sprintf("%d", proxy.ReadPort),
	}

	var err error
	err = psql.Connect()
	if err != nil {
		cluster.LogPrintf("ERROR", "%s", err)
		return
	}
	defer psql.Connection.Close()

	for _, s := range cluster.servers {
		switch s.State {
		case stateMaster:
			err = psql.SetWriter(s.Host, s.Port)
			if err != nil {
				cluster.LogPrintf("ERROR", "ProxySQL could not set %s as writer (%s)", s.URL, err)
			}
		case stateSlave:
			err = psql.SetReader(s.Host, s.Port)
			if err != nil {
				cluster.LogPrintf("ERROR", "ProxySQL could not set %s as reader (%s)", s.URL, err)
			}
		case stateFailed:
			// Let ProxySQL handle that case
		case stateUnconn:
			err = psql.SetOfflineHard(s.Host, s.Port)
			if err != nil {
				cluster.LogPrintf("ERROR", "ProxySQL could not set %s as offline (%s)", s.URL, err)
			}
		}
	}
	err = psql.LoadServersToRuntime()
	if err != nil {
		cluster.LogPrintf("ERROR", "ProxySQL could not load servers to runtime (%s)", err)
	}
}

func (cluster *Cluster) refreshProxysql(proxy *Proxy) {
	if cluster.conf.ProxysqlOn == false {
		return
	}

	psql := proxysql.ProxySQL{
		User:     proxy.User,
		Password: proxy.Pass,
		Host:     proxy.Host,
		Port:     proxy.Port,
		WriterHG: fmt.Sprintf("%d", proxy.WritePort),
		ReaderHG: fmt.Sprintf("%d", proxy.ReadPort),
	}

	var err error
	err = psql.Connect()
	if err != nil {
		cluster.LogPrintf("ERROR", "%s", err)
		return
	}
	defer psql.Connection.Close()

	var updated bool
	for _, s := range cluster.servers {
		s.ProxysqlHostgroup, s.MxsServerStatus, s.MxsServerConnections, err = psql.GetStatsForHost(s.Host, s.Port)
		s.MxsServerName = s.URL
		if err != nil {
			s.MxsServerStatus = "REMOVED"
		}

		// if ProxySQL and replication-manager states differ, resolve the conflict
		if s.MxsServerStatus == "OFFLINE_HARD" && s.State == stateSlave {
			cluster.LogPrintf("DEBUG", "ProxySQL setting online rejoining server %s", s.URL)
			err = psql.SetReader(s.Host, s.Port)
			if err != nil {
				cluster.LogPrintf("ERROR", "ProxySQL could not set %s as reader (%s)", s.URL, err)
			}
			updated = true
		}

		// if server is Standalone, set offline in ProxySQL
		if s.State == stateUnconn && s.MxsServerStatus == "ONLINE" {
			cluster.LogPrintf("DEBUG", "ProxySQL setting offline standalone server %s", s.URL)
			err = psql.SetOfflineHard(s.Host, s.Port)
			if err != nil {
				cluster.LogPrintf("ERROR", "ProxySQL could not set %s as offline (%s)", s.URL, err)
			}
			updated = true

			// if the server comes back from a previously failed or standalone state, reintroduce it in
			// the appropriate HostGroup
		} else if s.PrevState == stateUnconn || s.PrevState == stateFailed {
			if s.State == stateMaster {
				err = psql.SetWriter(s.Host, s.Port)
				if err != nil {
					cluster.LogPrintf("ERROR", "ProxySQL could not set %s as writer (%s)", s.URL, err)
				}
				updated = true
			} else if s.IsSlave {
				err = psql.SetReader(s.Host, s.Port)
				if err != nil {
					cluster.LogPrintf("ERROR", "ProxySQL could not set %s as reader (%s)", s.URL, err)
				}
				updated = true
			}
		}
	}
	if updated {
		err = psql.LoadServersToRuntime()
		if err != nil {
			cluster.LogPrintf("ERROR", "ProxySQL could not load servers to runtime (%s)", err)
		}
	}
}
