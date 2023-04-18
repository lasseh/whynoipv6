# Using the Project

This guide provides instructions on how to set up and use the project.

## Setting Up the Database

1. Create a new database and user:
```
CREATE DATABASE whynoipv6;
CREATE USER whynoipv6 with encrypted password '<removed>';
GRANT ALL PRIVILEGES ON DATABASE whynoipv6 TO whynoipv6;
```

## Adding Extensions and Migrations

1. Add the required extension to the database:
```CREATE EXTENSION pgcrypto;```

2. Run the database migrations:  
```make migrateup```

## Installing and Running Services

1. Install the necessary services:
```
go install -v github.com/eest/tranco-list-api/cmd/tldbwriter@latest
```

2. Start the services:
```
tldbwriter -config=tldbwriter.toml
```

## Importing Data and Crawling Domains

1. Import data:
```v6manage import```

2. Crawl domains:
```v6manage crawl```


## Updating the MaxMind Geo Database

1. Download the latest MaxMind Geo database from the following link:
`https://github.com/P3TERX/GeoLite.mmdb/releases`

2. Replace the existing database file with the downloaded file to update the database.


