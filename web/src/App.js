import React, { useState, useEffect } from 'react';
import './App.css';

function App() {
  const [containers, setContainers] = useState([]);
  const [selectedContainer, setSelectedContainer] = useState(null);
  const [logs, setLogs] = useState([]);
  const [alerts, setAlerts] = useState([]);
  const [status, setStatus] = useState({});

  useEffect(() => {
    fetchContainers();
    fetchAlerts();
    fetchStatus();
    
    const interval = setInterval(() => {
      fetchContainers();
      fetchAlerts();
      fetchStatus();
      if (selectedContainer) {
        fetchLogs(selectedContainer.id);
      }
    }, 2000);

    return () => clearInterval(interval);
  }, [selectedContainer]);

  const fetchContainers = async () => {
    try {
      const response = await fetch('/api/containers');
      const data = await response.json();
      setContainers(data || []);
    } catch (error) {
      console.error('Failed to fetch containers:', error);
    }
  };

  const fetchLogs = async (containerId) => {
    try {
      const response = await fetch(`/api/containers/${containerId}/logs?tail=100`);
      const data = await response.json();
      console.log(data)
      setLogs(data || []);
    } catch (error) {
      console.error('Failed to fetch logs:', error);
    }
  };

  const fetchAlerts = async () => {
    try {
      const response = await fetch('/api/alerts');
      const data = await response.json();
      setAlerts(data || []);
    } catch (error) {
      console.error('Failed to fetch alerts:', error);
    }
  };

  const fetchStatus = async () => {
    try {
      const response = await fetch('/api/status');
      const data = await response.json();
      setStatus(data || {});
    } catch (error) {
      console.error('Failed to fetch status:', error);
    }
  };

  const selectContainer = (container) => {
    setSelectedContainer(container);
    fetchLogs(container.id);
  };

  const formatTime = (timestamp) => {
    return new Date(timestamp).toLocaleTimeString();
  };

  const getLogLevel = (message) => {
    const msg = message.toLowerCase();
    if (msg.includes('error') || msg.includes('err')) return 'error';
    if (msg.includes('warn')) return 'warn';
    if (msg.includes('fatal') || msg.includes('panic')) return 'fatal';
    return 'info';
  };

  return (
    <div className="app">
      <header className="header">
        <h1>⚡ Kapstra Docker Alert Forwarder </h1>
        <div className="status">
          <span className={`status-dot ${status.overall === 'healthy' ? 'green' : 'red'}`}></span>
          <span>Monitoring {containers.length} containers</span>
        </div>
      </header>

      <div className="main-content">
        <div className="sidebar">
          <div className="section">
            <h3>🐳 Containers</h3>
            <div className="container-list">
              {containers.map(container => (
                <div
                  key={container.id}
                  className={`container-item ${selectedContainer?.id === container.id ? 'selected' : ''}`}
                  onClick={() => selectContainer(container)}
                >
                  <div className="container-name">📦 {container.name}</div>
                  <div className="container-status">{container.state}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="section">
            <h3>🚨 Recent Alerts ({alerts.length})</h3>
            <div className="alerts-list">
              {alerts.slice(0, 10).map((alert, index) => (
                <div key={index} className={`alert-item ${alert.severity.toLowerCase()}`}>
                  <div className="alert-time">⏰ {formatTime(alert.timestamp)}</div>
                  <div className="alert-container">📦 {alert.container_name}</div>
                  <div className="alert-message">{alert.message.substring(0, 60)}...</div>
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="content">
          {selectedContainer ? (
            <div className="logs-container">
              <div className="logs-header">
                <h2>📋 {selectedContainer.name} Logs</h2>
                <button onClick={() => fetchLogs(selectedContainer.id)}>🔄 Refresh</button>
              </div>
              <div className="logs">
                {logs.map((log, index) => (
                  <div key={index} className={`log-line ${getLogLevel(log.message)}`}>
                    <span className="log-time">{formatTime(log.timestamp)}</span>
                    <span className="log-message">{log.message}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="welcome">
              <h2>🎯 Select a container to view logs</h2>
              <p>Choose a container from the sidebar to start monitoring its logs and alerts in real-time.</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default App;