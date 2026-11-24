# Third-Party Notices

CryptoEngine is distributed under the **Evaluation License** (see
[LICENSE](LICENSE)). It depends on the following open-source projects. Each
component remains under its own license; the Evaluation License does not
restrict those components.

This file is a summary. The authoritative license text for each dependency is
the one shipped with that dependency.

## Go core (`backend/core`)

| Component | License |
|---|---|
| `github.com/jackc/pgx/v5` | MIT |
| `golang.org/x/net` | BSD-3-Clause |
| `google.golang.org/grpc` | Apache-2.0 |
| `google.golang.org/protobuf` | BSD-3-Clause |

Other transitive modules are pinned in `backend/core/go.sum` and retain their
respective licenses.

## Python API (`backend/api`)

| Component | License |
|---|---|
| FastAPI | MIT |
| Uvicorn | BSD-3-Clause |
| Pydantic | MIT |
| `python-multipart` | Apache-2.0 |
| `grpcio` | Apache-2.0 |
| `protobuf` | BSD-3-Clause |
| SQLAlchemy | MIT |
| `psycopg2-binary` | LGPL-3.0-or-later (see the project for exact terms) |
| `websockets` | BSD-3-Clause |
| `bcrypt` | Apache-2.0 |
| PyJWT | MIT |

## Dashboard (`frontend/dashboard`)

| Component | License |
|---|---|
| Next.js | MIT |
| React / React DOM | MIT |
| Tailwind CSS | MIT |
| `lightweight-charts` | Apache-2.0 |
| `lucide-react` | ISC |
| `three` | MIT |
| `@react-three/fiber` | MIT |
| `@react-three/drei` | MIT |
| TypeScript | Apache-2.0 |
| ESLint | MIT |

## Infrastructure

| Component | License |
|---|---|
| PostgreSQL | PostgreSQL License |
| Official Docker images (PostgreSQL, Node.js, Python, Go) | Their respective licenses |

## Notes

- This repository does not redistribute third-party source code; it declares
  dependencies through the package manifests and lockfiles.
- If you redistribute this repository or any component, you are responsible for
  complying with the applicable third-party licenses.
