# Spark

Template for building websites that inspire you.

- easy way to create demo.michaelbruce.co to show client (with robots disabled)
- easy way to setup netlify cms to edit
- forkable (create new git repo from it and go)
- easy way to keep track of dependencies (packages-lock.json?)
- email mailing list/marketing? drip? mailchimp?
- user tracking -> panel to view stats? google analytics?

## TODO

- preview does not have tailwind.
- preview is not picking up values for index page in demo mode. (may be a demo thing..)

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
- static
  - Gatsby by convention will copy everything from /static into the /public build target
- markdown files in pages:
  - gatsby-transformer-remark as frontmatter and graphql makes the data available.
- gatsby-node.js
  - runs actions during develop/build: primarily used to build dynamic pages with createPages.
- src/pages
  - either js or md files - pages for the website, md are made from templates
- src/templates
  - template pages

## Common Actions

### Building a new CMS-editable page
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

### Adding a new component

- Create new file in `./src/components`

```
import React from "react"

const Button = ({ children }) => {
  return (
    <button className="py-4 px-4 bg-green-600">
      {children}
    </button>
  )
}

export default Button
```

- Import from pages using `import Button from "../components/Button"`
- Use inside the page with `<Button>Click me!</Button>`
- Other parameters can be named/passed in as tag attrs (children is special)

### Including a font (from Google Fonts or Fontsquirrel):

- Get inspired first with fontpair.co/typography.js to see something that looks good.
- `npm install typeface-open-sans --save`
- include `require('type-open-sans')` in `gatsby-browser.js`

## Technology

- Tailwind: CSS framework that makes you a better designer.
  - Uses postcss, autoprefixer & purgecss.
- Gatsby/React: JS framework for creating static sites, without static files.
  - Build sites using components -> generate a static site.
- Netlify CMS: static CMS tool that allows non-tech users to edit static sites.
- NVM (node version manager, uses .nvmrc to provide compatible version)

## Technology Information

- propTypes are inbuilt type checking for React (gatsby-netlify-cms uses them)
- graphql queries return to 'data' variable - not sure const name means anything to them.
- gatsby has plugins for graphql to hook into e.g gatsby-source-filesystem.
- const { frontmatter: home } = data.allMarkdownRemark.edges[0].node;
  - this means pull out frontmatter attr and name it home.
- Gatsby doesn't work too well with markdown inside src/pages (hot reload is flaky)

## Static Site Gens compared

- Gatsby is used on React website (well supported)

## Ideas (not yet/may never be included):

1. could be a clojurescript shadow-cljs app with purge css etc
   - downside is that this complicates build + adds deps
   - another downside is autocompletion doesn't work with hiccup
   + plusside is that hiccup is easier to work with

2. spark could be a single bash script like rollout

3. Include a Conventions section for things like BEM, JAMstack, component workflow etc.

4. provide favicon? provide svg? unsplash? fontpair.co? typography.js? typefaces.js (fontsquirrel+google fonts)

5. Setup react-helmet? (gatsby-plugin-react-helmet)

6. Setup gatsby-transformer-sharp (not at develop time to avoid android problems?)

7. Setup gatsby-plugin-favicon?

8. Setup new netlify app? `spark deploy?`

9. include netlify-cms-media-library-cloudinary && netlify-cms-media-library-uploadcare

## References

https://github.com/robertcoopercode/gatsby-netlify-cms
