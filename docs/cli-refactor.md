# CLI Refactor: Verb-First to Noun-First + Positional Args

**Status: COMPLETED**

## Node Commands

### Before

```
steep-repl init start <target> --from <source>
steep-repl init prepare --node <node>
steep-repl init complete --node <target>
steep-repl init cancel --node <node>
steep-repl init progress --node <node>
steep-repl init reinit --node <node>
steep-repl init merge --node-a <a> --node-b <b>
```

### After

```
steep-repl node start <target> --from <source>
steep-repl node prepare <node>
steep-repl node complete <target>
steep-repl node cancel <node>
steep-repl node progress <node>
steep-repl node reinit <node>
steep-repl node merge <node-a> <node-b>
```

## Snapshot Commands

### Before

```
steep-repl snapshot generate --source <node> --output <path>
steep-repl snapshot apply --target <node> --input <path>
```

### After

```
steep-repl snapshot generate <source-node> --output <path>
steep-repl snapshot apply <target-node> --input <path>
```

## Schema Commands

### Before

```
steep-repl schema capture --node <node>
```

### After

```
steep-repl schema capture <node>
```

## TLS Commands

### Before

```
steep-repl init-tls [--output <path>] [--hosts <hosts>]
```

### After

```
steep-repl tls init [--output <path>] [--hosts <hosts>]
```

## Standalone Commands

### Before

```
steep-repl analyze-overlap --node-a <conn-a> --node-b <conn-b> --tables X,Y
steep-repl merge --node-a <conn-a> --node-b <conn-b> --tables X,Y
```

### After

```
steep-repl analyze-overlap <node-a-conn> <node-b-conn> --tables X,Y
steep-repl merge <node-a-conn> <node-b-conn> --tables X,Y
```

## Summary of Changes

1. Renamed the parent command from `init` to `node`
2. Converted `--node` flags to positional arguments to avoid stutter (e.g., `node ... --node`)
3. For commands with two nodes, both are now positional: `<node-a> <node-b>`
4. Renamed `init-tls` to `tls init` for noun-verb consistency
5. Applied consistent pattern across all commands: node IDs and primary targets are positional, configuration options remain as flags
