<!-- Static FAQ content for the "Rules, Frequency, and API Access" section, extracted verbatim
     from FAQ.vue, which owns the tab state and sidebar. -->
<template>
  <div>
    <div class="mb-8">
      <h1 class="h2 mb-4">Rules, Frequency, and API Access</h1>
    </div>
    <ul class="-my-4">
      <li class="pt-4">
        <h2 class="text-xl font-medium underline">Crawler</h2>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Crawler Rules</h3>
        <p class="text-base text-gray-400 mb-2">
          The crawler checks AAAA records for domain.com, www.domain.com, and the hosts named by the
          domain's NS and MX records. It also opens a real HTTP connection over IPv6; an AAAA record
          that doesn't answer won't pass the reachability check.
        </p>
        <p class="text-base text-gray-400">
          The domain and www lookups go through three independent public resolvers, and two out of
          three must agree.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Crawler Frequency</h3>
        <p class="text-base text-gray-400">
          Active domains normally run once per day. Base or www errors and disagreements can trigger
          faster retries or longer backoff. First definitive observations publish immediately. Later
          changes need two agreeing scans for DNS and mail, or three for reachability and page
          resources, so one flaky answer won't flip the verdict.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">What does “Not applicable” mean?</h3>
        <p class="text-base text-gray-400 mb-2">
          There was nothing to grade — it never counts against a domain.
        </p>
        <p class="text-base text-gray-400 mb-2">
          For Mail (MX), it means there is no mail host to grade: a null MX, a subdomain without
          explicit MX records, or no usable implicit MX fallback. The domain can still become a
          Hero.
        </p>
        <p class="text-base text-gray-400">
          For Page resources it means one of two things: no live external resource host remained to
          grade, or the site isn't reachable over IPv6 at all — then its resources can't be
          evaluated. The domain status card and the live check both spell out which one applies.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Why does a domain list subdomains?</h3>
        <p class="text-base text-gray-400 mb-2">
          An apex can score green while the part people actually use, the login portal or the API,
          is still IPv4 only. Anyone can list those hosts for a domain, and the crawler then checks
          them exactly like any other domain.
        </p>
        <p class="text-base text-gray-400">
          Subdomain results are informational: they never change the parent domain's rating or any
          of the country and campaign numbers. What gets listed depends on who took the time to list
          it, and a domain should not score worse for having attentive users. Add one by opening a
          PR on the
          <a href="https://github.com/lasseh/whynoipv6-campaign" target="_blank" class="a-gradient"
            >campaign repo</a
          >.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Crawler Errors</h3>
        <p class="text-base text-gray-400">Found a bug in the crawler? PRs are welcome.</p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Heroes</h3>
        <p class="text-base text-gray-400">
          Hero status requires an AAAA record on the tracked hostname, IPv6 on at least one
          nameserver host, and a site that answers over IPv6. The www and mail-host checks must pass
          too when they apply.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Saints</h3>
        <p class="text-base text-gray-400">
          Saints are Heroes whose page-resource grade is Supported or Not applicable. The crawler
          grades up to 50 external hosts found in the page; it does not fetch every resource over
          IPv6.
        </p>
      </li>
      <li class="pt-4">
        <h2 class="text-xl font-medium underline">Campaign Crawler</h2>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">How do I create my own campaign?</h3>
        <p class="text-base text-gray-400">
          Open an issue on the
          <a href="https://github.com/lasseh/whynoipv6-campaign" target="_blank" class="a-gradient"
            >GitHub repo</a
          >.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">How can I get my domain removed from the list?</h3>
        <p class="text-base text-gray-400">Yes, you can start using IPv6!</p>
      </li>
      <li class="pt-3">
        <h2 class="text-xl font-medium underline">API</h2>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Can I get access to the API?</h3>
        <p class="text-base text-gray-400 mb-2">
          Yes, the API is open — no key, no signup. Everything on this site is served from it, at
          <span class="a-gradient">https://api.whynoipv6.com</span> (no version prefix).
        </p>
        <p class="text-base text-gray-400 mb-2">
          Start with the
          <a href="https://api.whynoipv6.com/docs" target="_blank" class="a-gradient"
            >interactive docs</a
          >, the raw
          <a href="https://api.whynoipv6.com/openapi.json" target="_blank" class="a-gradient"
            >OpenAPI spec</a
          >, or — if you're pointing an LLM agent at the data —
          <a href="https://api.whynoipv6.com/llms.txt" target="_blank" class="a-gradient"
            >llms.txt</a
          >.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Can I download the whole dataset?</h3>
        <p class="text-base text-gray-400 mb-2">
          Yes — daily snapshots (CSV and Parquet) are published at
          <a href="https://api.whynoipv6.com/datasets" target="_blank" class="a-gradient"
            >api.whynoipv6.com/datasets</a
          >. Please don't paginate the whole API when a bulk file exists.
        </p>
        <p class="text-base text-gray-400">
          The data is licensed CC-BY-NC-4.0. Attribution: Data: whynoipv6.com (CC-BY-NC-4.0). Ranks:
          Tranco.
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Is there a badge for my README?</h3>
        <p class="text-base text-gray-400 mb-2">
          Every domain has an SVG status badge. Embed it in markdown:
        </p>
        <p class="text-base text-gray-400 font-mono">
          ![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)
        </p>
      </li>
      <li class="py-4">
        <h3 class="text-xl font-medium mb-2">Can I follow changes as a feed?</h3>
        <p class="text-base text-gray-400">
          The changelog is available as
          <a href="https://api.whynoipv6.com/changelog.atom" target="_blank" class="a-gradient"
            >Atom</a
          >
          and
          <a href="https://api.whynoipv6.com/changelog.feed.json" target="_blank" class="a-gradient"
            >JSON Feed</a
          >, with per-domain, per-country, and per-campaign variants — see the docs.
        </p>
      </li>
    </ul>
  </div>
</template>
