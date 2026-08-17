<!-- Static FAQ content for the "About" section, extracted verbatim
     from FAQ.vue, which owns the tab state and sidebar. -->
<template>
  <div>
    <div class="mb-8">
      <h1 class="h2 mb-4">About</h1>
    </div>
    <ul class="-my-4">
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2"># whoami</h2>
        <p class="text-base text-gray-400 mb-2">
          I'm Lasse, a network engineer from Norway. I build and run networks. I also run a crawler
          that keeps finding billion-dollar companies without an AAAA record.
        </p>
        <p class="text-base text-gray-400 mb-2">
          None of it is personal. A domain leaves the Sinners list once a globally routable apex
          AAAA record is confirmed. Becoming a Hero takes working IPv6 across the rest of the
          required checks too.
        </p>
        <p class="text-base text-gray-400">
          The endgame is an empty Sinners list. IPv6 turns 30 in 2028. I'd like to be done before it
          turns 40.
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">Contact</h2>
        <p class="text-base text-gray-400">
          Email:
          <span class="a-gradient">whynoipv6@protonmail.com</span>
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">Status page</h2>
        <p class="text-base text-gray-400 mb-2">
          Uptime and incident history:
          <a href="https://status.whynoipv6.com/" target="_blank" class="a-gradient"
            >status.whynoipv6.com</a
          >
        </p>
      </li>
      <li class="py-4">
        <h2 class="text-xl font-medium mb-2">Our Supporters</h2>
        <p class="text-base text-gray-400 mb-4">
          These organizations supported the site early on, back when it was one crawler and a
          grudge.
        </p>
        <!-- Sponsors -->
        <div
          class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-5 lg:col-span-6 mb-8 md:mb-0 md:order-1"
        >
          <div class="items-center">
            <a href="https://blix.com/" target="_blank"
              ><img
                class="md:block md:max-w-none"
                src="/images/Blix.webp"
                :width="350"
                :height="66"
                alt="Blix"
            /></a>
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>
