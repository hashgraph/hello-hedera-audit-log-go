# README

This readme is intended to guide the user through running the AdsDax / Hedera Consensus Service tracking demo application. As you may have read about in our previous blog post (see [here](https://www.hedera.com/blog/how-adsdax-built-a-scalable-decentralized-message-queue-with-hedera-hashgraph "How AdsDax built a scalable decentralized message queue with Hedera Hashgraph")), our full platform makes use of message queues in order to ensure we have a robust, failure-resistant production deployment.

This demo is a some-what simplified version of our production logic in order to avoid requiring the user to install and manage any external dependencies outside of their Go installation.

The details of installing Golang on the user's system is beyond the remit of this demo, and as such we would recommend consulting the official installation documentation [here](https://golang.org/doc/install "Getting started with Go").

If you are completely new to Golang, one thing we would recommend is trying out the [GoLand](https://www.jetbrains.com/go/ "GoLand by JetBrains") IDE from JetBrains as it has some nice features that you may find useful when exploring both the demo and your own applications, such as auto-completion and automatic imports. Of course, if you already have a preferred IDE or even prefer to work exclusively in the terminal then that is also fine.

#### Quick Guide

In order to get you up and running as quickly as possible, the demo application ships with some pre-filled credentials in the `demo.env` file that work on the Hedera testnet. If that account has been drained, or if you would prefer to create your own topic, we would recommend that you replace the `OPERATOR_ID` and `OPERATOR_KEY` with your own credentials, and remove the `TOPIC_*` values, like so:

```
OPERATOR_ID="{YOUR ACCOUNT ID E.G. 0.0.12345}"
OPERATOR_KEY="{THE SECRET KEY ASSOCIATED WITH THIS ACCOUNT ID, INCLUDING "302e..." PREFIX}"

TOPIC_ID=""
TOPIC_ADMIN_KEY=""
TOPIC_SUBMIT_KEY=""
```

If you have already created a Hedera Consensus Service Topic that would like to use, you can fill in the `TOPIC_ID`, `TOPIC_ADMIN_KEY` and `TOPIC_SUBMIT_KEY` values (as with the operator key, the Topic admin and submit keys both expect an Ed25519 Private Key to be provided). If the `TOPIC_*` keys are left blank, then when the demo application first runs it will automatically create a Topic for you to use.

In order to run the application you will need to install some of the packages that are listed in the `import` section of the `main.go` file. This can be done via the terminal by moving into the demo directory and running the following command, which should install and update all of the necessary dependencies:
```
go get -u ./...
```

When running the demo application, it starts a simple web-server that by default listens to `localhost:8080`. If required, the port used can be adjusted in the `main.go` file by editing the value of `portToUse` on line 26. After editing the port number (if necessary, the rest of this readme will assume the default value of `8080` is used), you can run the demo application using the following command (again, whilst in the demo application folder):
```
go run main.go
```

After running the demo, you should see the following response in the terminal:
```
Now listening on localhost:8080
```

You can then use a browser of your choice to visit [http://localhost:8080](http://localhost:8080 "Default AdsDax Demo URL") where you will see the log viewer, a video player, a play button and a text input field where you can optionally enter a secret message which will be encrypted before sending to the Hedera network.

Once you press the play button, you will see log messages as events related to the video playback are tracked. The white messages appear as events happen locally on the client. 

Blue messages are logged as events are sent to the Hedera Consensus Service and show the data in the same encrypted format as is sent to the Hedera network.

Orange messages appear as the events are received from the Hedera Consensus Service, and include augmented message information such as the Consensus Timestamp and message Sequence Numbers. The orange messages also show the decrypted message information, demonstrating the end-to-end process of securing non-readable, encrypted information on the ledger whilst maintaining your standard business logic within your application.

Extra
_____

Once you have become familiar with the demo application, we would recommend experimenting with the demo application logic to try the Consensus Service out for yourself. For instance, you could try passing additional information as part of the messages, changing the message format or change some of the functionality of the demo, such as recording how much of an article the user has read.



# In Depth

This demo, while not an exact replica of the live AdsDax platform, gives a broad overview of the logical processes we follow when handling and tracking data. As such, it is worth exploring the demo in detail to explore some of the processes we follow.

#### The `demo.html` file

The `demo.html` file contains the visual content of the demo that the user sees when visiting `localhost:8080`. For an actual user viewing pages on the AdsDax's Publisher network, the only part of this page they would actually see is the video player (or possibly an image if we were showing a display advert). For these users, the tracking process happening in the background is largely invisible, and would only be viewable if they happened to be using a web inspector or other development tool to view the network traffic as events are sent and feedback is received from our server.

When the user clicks play to start the video, the demo tracks events broadly in-line with the Video Ad Serving Template (a.k.a. VAST, see more [here](https://iabtechlab.com/standards/vast/ "IAB Tech Lab, VAST standard")), however the demo player isn't intended to be a full implementation of the VAST standard, instead only tracking the `start`, `complete` and `quartile` events.

###### The Track Route
______________________

As you view the video, you will see the log section on the left of the page begins to fill with white-coloured log messages as events are tracked locally. When this happens, the demo gathers some data from the client and sends it to `localhost:8080/track` with the following information passed as URL parameters:
```
localTimestamp = This is the timestamp as recorded on the user's machine at the time the event was tracked

tzOffset       = This is the timezone offset as reported in minutes via JavaScript. If the user's system clock is 
                 set to PST for example, it would show 480 minutes due to the timezone being UTC-8

videoUrl       = This is the URL of the video being played. By default the demo uses one of three publicly hosted 
                 videos, however if you would like to experiment and use your own video/s you can edit the video
                 array on line 79 of the demo.html file
           
videoCT        = This is the current time as reported via JavaScript using the currentTime attribute of the video
                 element
                 
videoDuration  = This is the full length of the video obtained via the duration property of the video element after
                 the video has loaded 
                 
event          = This is the name of the event we are tracking. START is tracked when playback begins, COMPLETE is
                 tracked when the video element dispatches the 'ended' event, FIRSTQUARTILE is tracked when as the
                 user views the first 25% of the video, with MIDPOINT at 50% and THIRDQUARTILE at 75%
                 
additionalInfo = This is the information the user can enter in the input field below the video player and log viewer.
                 This is optional and if left blank will just pass an empty string ("")
                 
userAgent      = This is the user agent of the device as reported via the userAgent field of the navigator JavaScript
                 object. If you try using the developer tools or other extensions to mask your user agent, you can see
                 that the value passed will change.                                  
```

Once this information is passed into the `localhost:8080/track` route, it is parsed and reformatted into the message we submit to the Hedera Consensus Service. Once this message has been submitted, the track route returns the Hedera transaction ID back to the client so we can then use that as part of the call to `localhost:8080/retrieve`.

###### The Retrieve Route
_________________________

Within the demo application, we attempt to display log information as close to the point at which it happens. This creates a potential issue as we want to return the encoded message that is submitted to the Consensus Service straight away, but we also want to be able to display the resulting message after it has passed consensus in the Hedera network, and been allotted a Sequence Number and Consensus TimeStamp.

In order to do this, as information is returned from our simple web-server after each call to `localhost:8080/track`, we take the Hedera transaction ID that is returned and begin another call from the client to our web-server on the `localhost:8080/retrieve` route, again passing the transaction ID as a parameter. 

On the server side, when a call to `localhost:8080/retrieve` is made the application checks whether the message that was initially sent has reached consensus by seeing if it is stored in the `eventStore` in `main.go` (see line no. 48). If the event is still awaiting consensus or our Topic subscriber hasn't finished processing it yet, the server will sleep for 250 milliseconds before checking the `eventStore` again, a process which repeats until the processed message is retrieved.

Whilst we could return a negative-response from the server and have the client attempt to repeat the `/retrieve` call, we felt this was a better method to follow as it results in fewer requests being shown in the network panel.

#### The `demo.env` file

The `demo.env` file exists as a nice way of storing configuration variables that we use in the `main.go` application logic. These variables are loaded when the `init()` call is made in the demo application (which happens prior to the `main()` call), with the loading handled by the `godotenv` module. This also encourages the user to separate the storage of application logic from potentially confidential information such as account numbers and private keys.

In the `demo.env` we store the following information:
```
OPERATOR_ID          = This is the account number we use to both create transactions and pay the associated fees for 
                       transactions

OPERATOR_KEY         = This is the Ed25519 Private Key associated with the above account ID.

TOPIC_ID             = This is the Topic ID we will submit messages to. It follows the same format as account IDs in 
                       that is uses {Realm Number}.{Shard Number}.{Topic Number}
               
TOPIC_ADMIN_KEY      = The Ed25519 Private Key used to manage the Topic (such as deleting it)

TOPIC_SUBMIT_KEY     = The Ed25519 Private Key used when submitting messages to a Topic (is used to prevent nefarious 
                       users from spamming other users' topics)

TOPIC_ENCRYPTION_KEY = This is a 32-byte string (note the string doesn't have to contain 32 characters if you are 
                       using multibyte characters) which we use to encrypt our messages to the AES-256 standard. If
                       you want to reduce the burden of encrypting and decrypting AES-256 messages, you can instead 
                       opt for 16 or 24 byte keys for AES-128 or AES-192 security respectively

MIRROR_ADDR          = This is the address of the mirror node that we will use to subscribe for updates to our Topic.
                       The default value for this is set to use the official Hedera Hashgraph testnet mirror node, 
                       however you could update this for use on the mainnet or to experiment with using a third-party
                       hosted mirror node
```

The `demo.env` file is already filled in by default with credentials for use on the Hedera testnet, however the Operator account balance may become depleted over time, in which case you would need to replace these values with your own testnet account credentials. If you wish to create your own Topic for use (whether using the supplied credentials or your own), by deleting the `TOPIC_ID`, `TOPIC_ADMIN_KEY` and `TOPIC_SUBMIT_KEY` values, e.g.
```
TOPIC_ID=""
TOPIC_ADMIN_KEY=""
TOPIC_SUBMIT_KEY=""
```
then a new Topic will be created the next time the demo application is run.

#### The `main.go` file

The `main.go` file contains the core application logic for interacting with the Hedera network via the official [Hedera Go SDK](https://github.com/hashgraph/hedera-sdk-go "Hedera Hashgraph SDK for Go"). The SDK is added as a dependency of the application in the `imports ()` section of the `main.go` file (lines 3-23). Some of the imported modules are basic modules that are included as part of the Go installation, such as the `fmt`, `strings` and `time` modules. We also use some third-party modules in the application, such as the `godotenv` (see [here](https://github.com/joho/godotenv "joho/godotenv on GitHub")) module which helps with nicely loading our `demo.env` file and the variables within, the `gjson` (see [here](https://github.com/tidwall/gjson "tidwall/gjson on GitHub")) and `sjson` (see [here](https://github.com/tidwall/sjson "tidwall/sjson on GitHub")) modules to nicely interact with JSON strings.

After the imports, we set up some global variables which we use to store information in allowing us to use it across different functions without having to duplicate the logic where those values are set (for instance, we want to avoid repeating the conversion and error handling logic where we convert the string based private keys from the `demo.env` files into `hedera.Ed25519PrivateKey` structs). Most of this logic happens in the `init()` call, so when the `main()` function is called we can safely assume that variables or set or relevant error information has been displayed to the user.

###### The `main()` function
____________________________

The `main()` function for the demo application is fairly slight as most of the logic happens in the various functions that get called as a result of user interactions. In the `main()` function we set up some of the routes that the user will be able to access on our simple web-server, start our Topic subscriber by calling `subscribeToTopicUpdates()` (which defaults to the Topic in the `demo.env` file) and then start our web-server by calling `http.ListenAndServe`.

It is worth noting that the order of these calls is quite important, as the routes we listen to need to be added before we start the web-server. Also, as the web-server blocks the main thread from processing any further (any logic written after this call will not fire while the server is active), we need to start the Topic subscriber before we start the web-server. This blocking of the main thread has some beneficial side effects for us which will be covered later when we look at the `subscribeToTopicUpdates()` function in-depth.

###### The `createTopic()` function
___________________________________

This function gets called from the `init()` function if the `demo.env` `TOPIC_*` keys are left blank. It is fairly straight-forward in that it generates the Admin and Submit secret keys for our new Topic, and then builds and signs the transaction before submitting it to the Hedera network. As we submit the transaction, we check for any errors or failures and then fetch the receipt via the `{transactionId}.GetReceipt()` call. From the receipt data, we get the `ConsensusTopicID()`, and then write all of this information back into our `demo.env` file for use on the next run of the demo (we wrote a custom `niceWrite()` function to do this as the default `godotenv.Write()` function was removing the comments from the `demo.env` file).

###### The `encryptText()` and `decryptText()` functions
________________________________________________________

The `encryptText()` and `decryptText()` functions are used to manage converting the private data we store in our messages both to and from plain, human-readable text. As mentioned, depending on the number of bytes in the encryption-key (which gets converted into a byte-array by these functions), different levels of security.

If a nefarious actor were to try and brute-force crack the encryption on our messages, it would take them many more computing cycles to crack longer key-lengths which then has the knock-on of increasing the energy consumption and costs associated with the attack. The downside of using increased key-lengths is that they are also slightly less-efficient when encrypting and decrypting messages, so if you wish to use encryption within your application you may need to factor in whether you want higher security or faster application performance.

We have opted to use a mix of public and private data (you can see the AdsDax topic on the testnet via the Kabuto explorer [here](https://explorer.kabuto.sh/testnet/id/0.0.147228 "AdsDax testnet HCS topic on kabuto.sh")) as whilst both ourselves and our advertising partners see the need for increased transparency in the advertising eco-system, being fully transparent with all event data has several issues which include:

+ Legal restrictions around who has the permission to view and process user data, how *personally identifiable information* (PII) is handled and the right to be forgotten (such as the GDPR restrictions throughout Europe)

+ The impacts of public price information on third-party aftermarkets such as within *Real-Time Bidding* applications. As a competing advertiser, if all of your bid history was publicly available (and I could essentially know what you are willing to pay for certain placements), I can ensure that my bid always wins. On the flipside, if I am a nefarious website, I can compete with you as an advertiser by bidding on my own traffic, thereby pushing the price up and increasing my own profit margin at the expense of your campaign ROI.

+ It stops any third-party entity from scraping all of the advertising data and potentially being able to track individual users or see their interests based on advertising targeting metrics or placement information.


  This is one of the main reasons we at AdsDax have been excited about the release of the Hedera Consensus Service, as we can have all of our events tracked on a 1:1 basis, signed off by the ledger with consensus timestamps and the fair-ordering guaranteed by the aBFT Hashgraph algorithm whilst also decoupling the tracking element of our operations from the payment element.
  
  This is very important for some of our advertisers given the sensitivity of price information, not to mention that the legal standing of crypto-currency transfers in some regions is still questionable. For instance, at the time of writing there is still an on-going court case in India concerning the *Reserve Bank of India's* stance on the legality of owning, possessing and transferring crypto-currencies and crypto-assets.
  
  Being able to separate these concerns and approach large markets without the threat or concern surrounding legal issues is one area where we feel the Consensus Service really excels when compared to the existing Cryptocurrency Service.

###### The `subscribeToTopicUpdates()` function
_______________________________________________

The `subscribeToTopicUpdates()` function is a fairly bare-bones and only implements the necessary logic required to receive messages from our Topic as they reach consensus. Processed messages get handed off to the `hcsMessageResponseHandler` function, with any errors getting passed to the `hcsMessageErrorHandler` function instead.

One important thing to note about subscribing to Topics when building your own application is that your program must stay-alive for the subscriber to carry on receiving messages. In the demo, this happens as a by-product of us starting the web-server, which keeps the application alive in order to listen for incoming connections.

An alternative to this however is to implement an infinitely repeating loop with a sleep timer, which then stops the main thread from finishing processing. You can do this like so:
```
for {
    time.Sleep(100 * time.Millisecond)
}
fmt.Printf("You should never see this message as code following the loop won't run!")
```

###### The `hcsMessageResponseHandler()` function
_________________________________________________

The `hcsMessageResponseHandler()` function receives incoming `hedera.MirrorConsensusTopicResponse` objects as they are dispatched by the subscriber. When building your own application, this is where you will likely have the widest divergence from the demo application logic. You may want to store your processed messages in a database, display them to an end user or even reference them in other Hedera services (for example, you could make a transfer where the `memo` points to a Consensus Service message that contains an itemised list of the items you are paying for).

In the case of the demo, however, we want to display the messages back to the user. To do this, we first get the additional information given to use by the Consensus Service and append this to the message contents (in our case, by adding the "hcs" field to our JSON object).

We also retrieve the "private" section of the message (this is the section that gets encrypted before we send the message to the Consensus Service), and use our `decryptText()` function to restore the human-readable information, adding this back into the JSON object.

One thing to note which you may want to carry through into your own applications is that when handling the encrypted data, we encode it with base64 encoding, as this renders nicely both on the explorer and within other monitoring tools we use, compared to the raw bytes often being rendered as weird glyph characters.

Once we have finished processing the message, we add it to our `eventStore` using the transaction ID as the map key, which is how the data is then accessed and sent back to the client by the `/retrieve` route.

###### The Page Handlers
________________________

There are several page handlers towards the bottom of the `main.go` file which are the functions that get called depending on the route that the user hits. The most simple of these is the `demoPageHandler` which simply returns the `demo.html` file contents which then are rendered in the browser.

As mentioned, the `retrieveHandler` is also fairly simplistic. When the user hits this route with a transaction ID as the parameter, it will repeatedly check the `eventStore` to see if there is an event matching the transaction ID.

The `trackingHandler` has some slightly more complex logic. First we gather the parameters from the URL, which we then use to populate a new instance of our `HcsMessageStruct{}`. This struct is used to more easily reformat the parameters into our desired JSON format via the `json.Marhsal()` function.

You can examine the struct in the `main.go` file starting on line 393. If you wanted to experiment by editing the format of the messages that are sent to the Consensus Service, we would recommend starting by editing this struct and then changing the values that are set in the `trackingHandler` around line 458.

Once we have reformatted the event parameter data into JSON, we then pass the "private" information to the `encryptText()` function, using the `gjson` package to fetch this field.

We then take the encrypted data and place a base64 encoded version of it into the JSON string data using the `sjson` package, overriding the "private" field in the JSON data.

One additional noteworthy thing we do in the demo is utilise the ability to set the transaction ID of a transaction before it's built. This has the benefit that anyone who wishes to audit the message data can then also examine the transaction records without having to rely on a third-party explorer to provide this link.

After submitting the transaction to the network, we then return information to the client by calling `fmt.Fprint()`, passing in our responseWriter `rw` and the message we want to write as arguments.



# Summary

We at AdsDax hope that this demo application will not only demonstrate a useful, real-life use case for the Hedera Consensus Service but also provide the impetus for more people to explore developing and experimenting with the Hedera Consensus Service and the Hedera Hashgraph network in general. If you have any issues with this demo application or indeed developing on the Hedera Hashgraph network, we heartily recommend joining the rest of the Hedera development community on the [Hedera Developer Discord Channel](https://discordapp.com/invite/FFb9YFX "Join the Hedera Hashgraph Discord group"). We look forward to exploring the Hedera network alongside you, and hope to have more exciting things to announce in future.