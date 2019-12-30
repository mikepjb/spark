# Spark

Template for building websites

- easy way to create demo.michaelbruce.co to show client (with robots disabled)
- easy way to setup netlify cms to edit
- forkable (create new git repo from it and go)
- easy way to keep track of dependencies (packages-lock.json?)
- email mailing list/marketing? drip? mailchimp?
- user tracking -> panel to view stats? google analytics?

## Getting Started

- install nvm (a version manager to select specific versions of node/npm).
- make sure you have `node` and `npm` available in your PATH. `nvm install 13`
- install gatsby-cli globally `npm i -g gatsby-cli`

## Layout

How is a website laid out when using spark?

- public/admin/config.yml
  - A configuration file for NetlifyCMS, describes collections of resources
    that can be edited. Usually this will be blog posts and pages (where pages
    will be pages with fields that can be edited).
- public/img/
  - An folder that NetlifyCMS commits images to on behalf of an administrator.

## Common Actions

- Building a new CMS-editable page
  - Requires a new src/cms/preview-templates/<name>.js
    - import navbar/page/component
    - assign proptypes? (whatever that is..)
    - export default x; (whatever that is..)
  - How does this match up with config.yml?

- Setting up Netlify side of things
  - ???
  - profit
  - Setup NetlifyCMS access
    - Enable Identity
    - Change Registration Preferences to Invite Only
    - Enable Git Gateway
    - Invite yourself and client

## Technology

- Tailwind: CSS framework that makes you a better designer.
  - Uses postcss, autoprefixer & purgecss.
- Gatsby/React: JS framework for creating static sites, without static files.
  - Build sites using components -> generate a static site.
- Netlify CMS: static CMS tool that allows non-tech users to edit static sites.
- NVM (node version manager, uses .nvmrc to provide compatible version)

## Static Site Gens compared

- Gatsby is used on React website (well supported)

## Ideas (not yet/may never be included):

1. could be a clojurescript shadow-cljs app with purge css etc
   - downside is that this complicates build + adds deps
   - another downside is autocompletion doesn't work with hiccup
   + plusside is that hiccup is easier to work with

2. spark could be a single bash script like rollout

3. Include a Conventions section for things like BEM, JAMstack, component workflow etc.
