<!-- Static FAQ content for the "Frequently Asked Questions" section, extracted verbatim
     from FAQ.vue, which owns the tab state and sidebar. -->
<template>
  <div>
    <div class="mb-8">
      <h1 class="h2 mb-4">Frequently Asked Questions</h1>
    </div>
    <ul class="-my-4">
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">What is Why No IPv6?</h2>
        <p class="text-base text-gray-400">
          Why No IPv6 crawls the active Tranco list on its normal daily schedule, plus
          user-submitted campaigns, and checks each entry for IPv6: the domain, www, nameserver
          hosts, mail hosts, and real web reachability. Failures can back off. Then we sort the
          results into Sinners, Heroes, and Saints and publish the receipts.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">Why does IPv6 matter?</h2>
        <p class="text-base text-gray-400 mb-2">
          IPv4 ran out. Not 'is running out': ran out. The regional registries held the funeral
          years ago. Networks still need addresses. IPv6 has them. For a top-ranked site today,
          skipping it isn't an oversight.
        </p>
        <p class="text-base text-gray-400">
          We check the top million and publish which domains have IPv6 and which don't. The second
          list is meant to be uncomfortable.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">How does the site work?</h2>
        <p class="text-base text-gray-400">
          The crawler works through the active Tranco list, checking AAAA records for the domain,
          www, nameserver hosts, and mail hosts, plus a real HTTP connection over IPv6. The results
          feed everything here: the tiers, country stats, and changelog.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">Tranco?</h2>
        <p class="text-base text-gray-400 mb-2">
          The
          <a href="https://tranco-list.eu/" target="_blank" class="a-gradient">Tranco List</a>
          ranks the top million domains by aggregating several traffic lists, which smooths out the
          noise and manipulation that made single-source rankings like Alexa easy to game. It's a
          ranking researchers actually use.
        </p>
        <p class="text-base text-gray-400">
          We use it because the rank is half the shame: 'top 100 site, zero AAAA records' only lands
          if the ranking is credible.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">How accurate is the data?</h2>
        <p class="text-base text-gray-400">
          Treat the data as indicative, not absolute. DNS propagation and CDNs that answer
          differently per anycast location can shift a result from one scan to the next. First
          definitive observations publish immediately; later changes need two agreeing scans for DNS
          and mail, or three for reachability and page resources.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">
          Why does a domain show as not supporting IPv6 when it does?
        </h2>
        <p class="text-base text-gray-400 mb-2">
          Usually DNS propagation lag, a CDN answering differently from our vantage point, or a
          server that didn't respond during that scan. A real change has to survive the confirmation
          window, so give it a few days. If it still looks wrong, contact us.
        </p>
        <p class="text-base text-gray-400">
          The crawler reports DNS and reachability separately. An AAAA record can be supported while
          the site still fails its IPv6 connection check.
        </p>
      </li>
    </ul>
  </div>
</template>
