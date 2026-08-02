<script setup lang="ts"></script>

<template>
  <footer>
    <div class="py-12 md:py-16">
      <div class="max-w-6xl mx-auto px-4 sm:px-6">
        <!-- Bottom area -->
        <div class="md:flex md:items-center md:justify-between">
          <!-- Social links -->
          <ul class="flex mb-4 md:order-1 md:ml-4 md:mb-0">
            <li>
              <a
                href="https://github.com/lasseh/whynoipv6"
                target="_blank"
                class="flex justify-center items-center text-fuchsia-600 bg-gray-800 hover:text-gray-100 hover:bg-fuchsia-600 rounded-full transition duration-150 ease-in-out"
                aria-label="GitHub"
              >
                <svg
                  class="w-8 h-8 fill-current"
                  viewBox="0 0 32 32"
                  xmlns="http://www.w3.org/2000/svg"
                >
                  <path
                    d="M16 8.2c-4.4 0-8 3.6-8 8 0 3.5 2.3 6.5 5.5 7.6.4.1.5-.2.5-.4V22c-2.2.5-2.7-1-2.7-1-.4-.9-.9-1.2-.9-1.2-.7-.5.1-.5.1-.5.8.1 1.2.8 1.2.8.7 1.3 1.9.9 2.3.7.1-.5.3-.9.5-1.1-1.8-.2-3.6-.9-3.6-4 0-.9.3-1.6.8-2.1-.1-.2-.4-1 .1-2.1 0 0 .7-.2 2.2.8.6-.2 1.3-.3 2-.3s1.4.1 2 .3c1.5-1 2.2-.8 2.2-.8.4 1.1.2 1.9.1 2.1.5.6.8 1.3.8 2.1 0 3.1-1.9 3.7-3.7 3.9.3.4.6.9.6 1.6v2.2c0 .2.1.5.6.4 3.2-1.1 5.5-4.1 5.5-7.6-.1-4.4-3.7-8-8.1-8z"
                  />
                </svg>
              </a>
            </li>
            <li class="ml-4">
              <a
                href="https://twitter.com/whynoipv6"
                target="_blank"
                class="flex justify-center items-center text-fuchsia-600 bg-gray-800 hover:text-gray-100 hover:bg-fuchsia-600 rounded-full transition duration-150 ease-in-out"
                aria-label="Twitter"
              >
                <svg
                  class="w-8 h-8 fill-current"
                  viewBox="0 0 32 32"
                  xmlns="http://www.w3.org/2000/svg"
                >
                  <path
                    d="M24 11.5c-.6.3-1.2.4-1.9.5.7-.4 1.2-1 1.4-1.8-.6.4-1.3.6-2.1.8-.6-.6-1.5-1-2.4-1-1.7 0-3.2 1.5-3.2 3.3 0 .3 0 .5.1.7-2.7-.1-5.2-1.4-6.8-3.4-.3.5-.4 1-.4 1.7 0 1.1.6 2.1 1.5 2.7-.5 0-1-.2-1.5-.4 0 1.6 1.1 2.9 2.6 3.2-.3.1-.6.1-.9.1-.2 0-.4 0-.6-.1.4 1.3 1.6 2.3 3.1 2.3-1.1.9-2.5 1.4-4.1 1.4H8c1.5.9 3.2 1.5 5 1.5 6 0 9.3-5 9.3-9.3v-.4c.7-.5 1.3-1.1 1.7-1.8z"
                  />
                </svg>
              </a>
            </li>
            <li class="ml-4">
              <a
                href="/blog/rss.xml"
                class="flex justify-center items-center text-fuchsia-600 bg-gray-800 hover:text-gray-100 hover:bg-fuchsia-600 rounded-full transition duration-150 ease-in-out"
                aria-label="Blog RSS feed"
              >
                <svg
                  class="w-8 h-8"
                  viewBox="-5 -4.5 34 34"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  xmlns="http://www.w3.org/2000/svg"
                >
                  <path d="M4 11a9 9 0 0 1 9 9" />
                  <path d="M4 4a16 16 0 0 1 16 16" />
                  <circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none" />
                </svg>
              </a>
            </li>
          </ul>

          <!-- Copyrights note -->
          <div class="text-gray-400 text-sm mr-4" data-nosnippet>
            <div>
              Numbers with sentences on
              <router-link to="/blog" class="underline whitespace-nowrap a-gradient"
                >the blog</router-link
              >
            </div>
            <div>
              Hosted on native IPv6 co-location at
              <a
                href="https://blix.com/"
                class="underline whitespace-nowrap a-gradient"
                target="_blank"
                >Blix Solutions</a
              >
              since 2008
            </div>
            <div>
              IP data by
              <a
                href="https://ipinfo.io"
                class="underline whitespace-nowrap a-gradient"
                target="_blank"
                >IPinfo</a
              >
            </div>
          </div>
        </div>
      </div>
    </div>
  </footer>
</template>
