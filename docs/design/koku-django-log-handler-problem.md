# Koku Django Log Handler — ReadOnlyRootFilesystem Blocker

## Problem

The koku container cannot run with `readOnlyRootFilesystem: true` because
Django's logging configuration instantiates a `FileHandler` at process startup
unconditionally.

### Root cause

`koku/koku/settings/base.py` (and the on-prem override) declares a logging
handler that writes to `/opt/koku/koku/app.log`:

```python
LOGGING = {
    "handlers": {
        "file": {
            "class": "logging.FileHandler",
            "filename": "/opt/koku/koku/app.log",
            ...
        },
        "console": { ... },
    },
    "root": {
        "handlers": ["console"],   # ← intended active handler
    },
}
```

Django iterates **all declared handlers** during `logging.config.dictConfig()`
and instantiates each `FileHandler` — including ones not attached to any
logger. The `FileHandler.__init__` opens (or creates) the file immediately,
even if that handler is never used.

Setting `DJANGO_LOG_HANDLERS=console` in the environment changes which handlers
loggers use, but Django still **instantiates** every handler in the `handlers`
dict during startup. This causes:

```
PermissionError: [Errno 13] Permission denied: '/opt/koku/koku/app.log'
```

when the root FS is read-only, because the file cannot be created.

## Impact

The operator cannot set `readOnlyRootFilesystem: true` on:
- Koku API container
- Masu container
- Listener container
- Celery Beat and all Celery worker containers
- Koku migration Job container

This means koku pods cannot reach full `restricted-v2` SCC compliance, which
requires `readOnlyRootFilesystem: true`. All other pods (ROS, Kruize, Ingress,
init containers, UI) already use read-only root FS.

## Operator workaround

`kokuAppContainerSC()` in `internal/resources/volumes.go` omits
`ReadOnlyRootFilesystem`:

```go
// ReadOnlyRootFilesystem absent: Django unconditionally instantiates
// FileHandler at /opt/koku/koku/app.log during logging.config.dictConfig(),
// even when that handler is not attached to any active logger. Fix in koku.
func kokuAppContainerSC() *corev1.SecurityContext { ... }
```

## Fix required in koku

The logging configuration in `koku/koku/settings/` should be changed so that
the `file` handler is only declared when `DJANGO_LOG_HANDLERS` includes `file`.
One approach:

```python
import os

_active_handlers = os.environ.get("DJANGO_LOG_HANDLERS", "file").split(",")

_all_handlers = {
    "console": { "class": "logging.StreamHandler", ... },
}
if "file" in _active_handlers:
    _all_handlers["file"] = {
        "class": "logging.FileHandler",
        "filename": "/opt/koku/koku/app.log",
        ...
    }

LOGGING = {
    "handlers": _all_handlers,
    "root": {
        "handlers": _active_handlers,
    },
}
```

With this change:
- When `DJANGO_LOG_HANDLERS=console` (operator default), `FileHandler` is never
  declared and never instantiated — no file write at startup
- `readOnlyRootFilesystem: true` becomes safe to set on all koku containers
- Full `restricted-v2` SCC compliance is achievable without any workaround

## Tracking

This should be filed against the koku application repository. The on-prem
operator sets `DJANGO_LOG_HANDLERS=console` in all koku env vars (`env.go`)
specifically to mitigate this, but the fix must happen in koku itself.
