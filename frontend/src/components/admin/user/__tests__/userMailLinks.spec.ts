import { describe, expect, it } from 'vitest'
import { currentSiteUserMailLinks } from '../userMailLinks'

describe('current site email destinations', () => {
  it.each(['oreniva.com', 'xenoai.eu.cc', 'us.xenoai.eu.cc'])('uses the exact current hostname %s', hostname => {
    expect(currentSiteUserMailLinks(`https://${hostname}/admin/users?search=x#mail`)).toEqual({
      platform_url: `https://${hostname}`,
      docs_url: `https://${hostname}/docs`,
      model_plaza_url: `https://${hostname}/model-plaza`,
      canvas_url: `https://canvas.${hostname}`
    })
  })

  it('keeps the site protocol and port while canvas uses HTTPS on its own host', () => {
    expect(currentSiteUserMailLinks('http://example.com:8080/admin/users')).toEqual({
      platform_url: 'http://example.com:8080', docs_url: 'http://example.com:8080/docs',
      model_plaza_url: 'http://example.com:8080/model-plaza', canvas_url: 'https://canvas.example.com'
    })
  })

  it.each(['http://localhost:8000/admin/users', 'http://127.0.0.1:8000/admin/users', 'http://[::1]:8000/admin/users'])('provides an editable local canvas default for %s', href => {
    expect(currentSiteUserMailLinks(href).canvas_url).toBe('https://canvas.localhost')
  })

  it('does not invent a canvas subdomain for a non-domain IP host', () => {
    expect(currentSiteUserMailLinks('https://192.0.2.1/admin/users')).toEqual({
      platform_url: 'https://192.0.2.1', docs_url: 'https://192.0.2.1/docs',
      model_plaza_url: 'https://192.0.2.1/model-plaza', canvas_url: ''
    })
  })
})
