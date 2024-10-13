# Using the Project

This guide provides instructions on how to set up and use the project.

## Setting Up the Database

1. Create a new database and user:
```
CREATE DATABASE whynoipv6;
CREATE USER whynoipv6 with encrypted password '<removed>';
GRANT ALL PRIVILEGES ON DATABASE whynoipv6 TO whynoipv6;

GRANT USAGE ON SCHEMA public TO whynoipv6;
GRANT CREATE ON SCHEMA public TO whynoipv6;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO whynoipv6;

CREATE EXTENSION pgcrypto;
```

# User
Create a new user and install folders
```bash
useradd -m -d /opt/whynoipv6/ ipv6
mkdir -p /opt/whynoipv6/{whynoipv6,whynoipv6-web,whynoipv6-campaign}
chown -R ipv6:ipv6 /opt/whynoipv6
````

## Clone
Clone all the repo's
```bash
su - ipv6
git clone https://github.com/lasseh/whynoipv6.git /opt/whynoipv6/whynoipv6/
git clone https://github.com/lasseh/whynoipv6-web.git /opt/whynoipv6/whynoipv6-web/
git clone https://github.com/lasseh/whynoipv6-campaign /opt/whynoipv6/whynoipv6-campaign/
```


## Install
1. Edit the env file:  
```bash
cp app.env.example app.env
vim app.env
```

1. Run the database migrations:  
```
make migrateup
```

2. Start Tranco List downloader  
Edit the tldbwriter.toml file with database details
```bash
cp tldbwriter.toml.example tldbwriter.toml
vim tldbwriter.toml
make tldbwriter
```
ctrl-c after: time until next check: 1h0m9s
 

## Importing Data and Crawling Domains

1. Copy the service files
```bash
cp /opt/whynoipv6/whynoipv6/extra/systemd/* /etc/systemd/system
systemctl daemon-reload
```

1. Import data:
```
/opt/whynoipv6/go/bin/v6manage import
/opt/whynoipv6/go/bin/v6manage campaign import
```

2. Start services
```
systemctl enable --now whynoipv6-api
systemctl enable --now whynoipv6-crawler
systemctl enable --now whynoipv6-campaign-crawler
```

# Frontend
Create folder for the html 
```bash
mkdir /var/www/whynoipv6.com/
chown ipv6:ipv6 /var/www/whynoipv6.com/ 
```

## Updating the MaxMind Geo Database

1. Download the latest MaxMind Geo database from the following link:
`https://github.com/P3TERX/GeoLite.mmdb/releases`

2. Replace the existing database file with the downloaded file to update the database.


# Monitor services
```bash
journalctl -o cat -fu whynoipv6-c* | ccze -A
journalctl -o cat -fu whynoipv6-api | ccze -A
```


# Grafana

1. Create a read-only sql user:
```sql
CREATE USER whynoipv6_read WITH PASSWORD '<removed>';
GRANT CONNECT ON DATABASE whynoipv6 TO whynoipv6_read;
GRANT USAGE ON SCHEMA public TO whynoipv6_read;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO whynoipv6_read;
```

1. Add the following to the `pg_hba.conf` file:
```
host  graph.domain.com  whynoipv6_read
```
