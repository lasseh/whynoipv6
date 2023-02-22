# Using the project

### Database
Create new database and user
```
CREATE DATABASE whynoipv6;
CREATE USER whynoipv6 with encrypted password '<removed>';
GRANT ALL PRIVILEGES ON DATABASE whynoipv6 TO whynoipv6;
```

## DB Func
```CREATE EXTENSION pgcrypto;```

### DB Migrations
Start the db migrations  
```make migrateup```

### Services

Install neeeded services
```
go install -v github.com/eest/tranco-list-api/cmd/tldbwriter@latest
```

Start services
```
tldbwriter -config=tldbwriter.toml
```

### Import data
```v6manage import```

## Crawl domains
```v6manage crawl```


### MaxMind Geo Database
`https://github.com/P3TERX/GeoLite.mmdb/releases`

How to update?
