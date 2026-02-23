TODO

- Fix #176
- Add agent configuration file parsing with an AgentConfig struct
- Wrap `Conn` objects returned by the listener with a type that can be used to determine if they are TLS-terminated or not
- Remove the legacy package by folding all of its logic into the current package
- Add an RPC test
- Implement support for AgentSession.ID()
